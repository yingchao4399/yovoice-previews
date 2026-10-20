package workbench

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
	"yovoice/internal/audio"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
	"yovoice/internal/score"

	"golang.org/x/net/html"
)

const openverseAudioURL = "https://api.openverse.org/v1/audio/"

var openverseID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

var freesoundSoundID = regexp.MustCompile(`^/people/[^/]+/sounds/([0-9]+)/?$`)
var freesoundMediaID = regexp.MustCompile(`^/(?:data/)?previews/[0-9]+/([0-9]+)(?:[_-][^/]*)?\.[A-Za-z0-9]+$`)
var numericPageID = regexp.MustCompile(`^[0-9]+$`)

type InstrumentCandidate struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Creator    string  `json:"creator"`
	Provider   string  `json:"provider"`
	LandingURL string  `json:"landingUrl"`
	License    string  `json:"license"`
	Duration   float64 `json:"duration"`
	Kind       string  `json:"kind"` // noteCandidate 或 phraseCandidate，均需下载后核验。
}

type OnlineSampleInput struct {
	ID                  string `json:"id"`
	Instrument          string `json:"instrument"`
	Articulation        string `json:"articulation"`
	SourceID            string `json:"sourceId,omitempty"`
	ConfirmedInstrument bool   `json:"confirmedInstrument"`
}

type openverseAudio struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Creator        string `json:"creator"`
	Provider       string `json:"provider"`
	Source         string `json:"source"`
	LandingURL     string `json:"foreign_landing_url"`
	URL            string `json:"url"`
	License        string `json:"license"`
	LicenseVersion string `json:"license_version"`
	LicenseURL     string `json:"license_url"`
	DurationMS     int    `json:"duration"`
	FileSize       int64  `json:"filesize"`
	FileType       string `json:"filetype"`
	Mature         bool   `json:"mature"`
	Tags           []struct {
		Name string `json:"name"`
	} `json:"tags"`
}

func (a openverseAudio) candidate() (InstrumentCandidate, bool) {
	if !openverseID.MatchString(a.ID) || a.License != "cc0" || a.LicenseVersion != "1.0" ||
		!strings.HasPrefix(a.LicenseURL, "https://creativecommons.org/publicdomain/zero/1.0/") ||
		a.Mature || a.DurationMS < 100 || a.DurationMS > 30000 || a.FileSize < 1 || a.FileSize > maxUpload ||
		!slices.Contains([]string{"wav", "flac", "mp3", "ogg", "aiff", "m4a"}, strings.ToLower(a.FileType)) {
		return InstrumentCandidate{}, false
	}
	if !validOnlineSource(a) || strings.HasPrefix(strings.ToUpper(a.Title), "LL-Q") || strings.Contains(strings.ToLower(a.Creator), "speaker:") {
		return InstrumentCandidate{}, false
	}
	kind := "noteCandidate"
	if slices.ContainsFunc([]string{"riff", "song", "melody", "loop", "phrase", "ensemble"}, func(word string) bool { return strings.Contains(strings.ToLower(a.Title), word) }) {
		kind = "phraseCandidate"
	}
	return InstrumentCandidate{ID: a.ID, Title: strings.TrimSpace(a.Title), Creator: strings.TrimSpace(a.Creator), Provider: a.Source,
		LandingURL: a.LandingURL, License: "CC0-1.0", Duration: float64(a.DurationMS) / 1000, Kind: kind}, true
}

func validOnlineSource(a openverseAudio) bool {
	media, err := url.Parse(a.URL)
	if err != nil || media.Scheme != "https" {
		return false
	}
	landing, err := url.Parse(a.LandingURL)
	if err != nil || landing.Scheme != "https" {
		return false
	}
	switch a.Source {
	case "freesound":
		page := freesoundSoundID.FindStringSubmatch(landing.Path)
		file := freesoundMediaID.FindStringSubmatch(media.Path)
		return media.Hostname() == "cdn.freesound.org" && landing.Hostname() == "freesound.org" && len(page) == 2 && len(file) == 2 && page[1] == file[1]
	case "wikimedia_audio":
		return media.Hostname() == "upload.wikimedia.org" && landing.Hostname() == "commons.wikimedia.org" && numericPageID.MatchString(landing.Query().Get("curid"))
	default:
		return false
	}
}

func onlineSearchTerm(instrument string) string {
	aliases := map[string]string{"琵琶": "pipa", "箫": "xiao flute", "古筝": "guzheng", "二胡": "erhu", "古琴": "guqin", "唢呐": "suona", "笛子": "dizi", "阮": "ruan lute"}
	if term := aliases[strings.TrimSpace(instrument)]; term != "" {
		return term
	}
	return strings.TrimSpace(instrument)
}

// 自动检索只接受标题明确写出乐器名称的 Freesound 单音候选；词义同名或乐句不会自动入库。
// 此检查只确认元数据，不声称能听辨真实乐器，因此导入后会标为待人工试听。
func autoTitleMatches(item openverseAudio, instrument string) bool {
	if item.Source != "freesound" || strings.TrimSpace(item.Title) == "" {
		return false
	}
	title := strings.ToLower(item.Title)
	for _, bad := range []string{"speech", "pronunciation", "wiktionary", "spoken", "language", "voice", "vocal", "sfx", "pipas"} {
		if strings.Contains(title, bad) {
			return false
		}
	}
	if strings.Contains(title, strings.ToLower(strings.TrimSpace(instrument))) {
		return true
	}
	term := strings.Fields(strings.ToLower(onlineSearchTerm(instrument)))[0]
	if regexp.MustCompile(`(^|[^a-z])`+regexp.QuoteMeta(term)+`([^a-z]|$)`).FindStringIndex(title) == nil {
		return false
	}
	return true
}

// AutoFindInstrument 在后台自主检索、复核原站 CC0、检查单音音高并按需下载。
// 元数据无法证明乐器音色真伪；下载结果明确标记为 metadata，用户可试听替换。
func (w *Workbench) AutoFindInstrument(instrument string) error {
	instrument = strings.TrimSpace(instrument)
	if instrument == "" || schema.TextLen(instrument) > 80 {
		return msg.Err(msg.ErrRequestInvalid, nil)
	}
	return w.begin("instrumentDownload", msg.ActivityDownload, msg.Params{"name": instrument}, nil, func(parent context.Context) error {
		ctx, cancel := context.WithTimeout(parent, 150*time.Second)
		defer cancel()
		var source schema.InstrumentSource
		attempted := 0
		checked := 0
		matched := 0
		var detailFailure, downloadFailure error
		for page := 1; page <= 3; page++ {
			candidates, more, err := w.searchInstrumentPage(ctx, instrument, page, true)
			if err != nil {
				if page == 1 {
					return err
				}
				break
			}
			for _, candidate := range candidates {
				if candidate.Kind != "noteCandidate" || attempted >= 6 || ctx.Err() != nil {
					continue
				}
				item, err := w.onlineCandidate(ctx, candidate.ID)
				if err != nil {
					detailFailure = err
					continue
				}
				checked++
				if !autoTitleMatches(item, instrument) {
					continue
				}
				matched++
				attempted++
				result, err := w.importOnlineInstrumentSample(ctx, OnlineSampleInput{ID: candidate.ID, Instrument: instrument, Articulation: "normal", SourceID: source.ID}, "metadata")
				if err != nil {
					downloadFailure = err
					continue
				}
				source = result
				w.progressOf("instrumentDownload", msg.ActivityDownloading, msg.Params{"name": instrument}, int64(len(source.Samples)), 3)
				if len(source.Samples) >= 3 {
					return nil
				}
			}
			if !more || attempted >= 6 || ctx.Err() != nil {
				break
			}
		}
		if len(source.Samples) > 0 {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if matched > 0 && downloadFailure != nil {
			return fmt.Errorf("找到匹配的 %s 音源，但下载或采样校验失败：%w", instrument, downloadFailure)
		}
		if checked == 0 && detailFailure != nil {
			return fmt.Errorf("已找到候选音源，但原站详情请求或许可校验失败：%w", detailFailure)
		}
		return fmt.Errorf("未找到可核验的 %s CC0 单音录音，请在音源库继续搜索或自行导入", instrument)
	})
}

func (w *Workbench) onlineBytes(ctx context.Context, rawURL, host string, limit int64) ([]byte, error) {
	// 在线寻源和固定目录下载必须共享同一套网络回退策略。否则设置里
	// 留着失效代理时，目录下载能成功而 Openverse/Freesound 自动寻源仍会失败。
	routes := w.catalogRouteOrder(w.catalogRoutes(catalogSystemProxy()))
	var failures []string
	for index, route := range routes {
		client, clientErr := w.catalogClient(route)
		if clientErr != nil {
			failures = append(failures, route.name+": "+clientErr.Error())
			continue
		}
		attemptCtx, cancel := context.WithTimeout(ctx, 25*time.Second)
		content, err := onlineBytesWithClient(attemptCtx, client, rawURL, host, limit)
		cancel()
		if err == nil {
			w.catalogPreferredRoute.Store(int32(index + 1))
			w.catalogSystemProxyPreferred.Store(route.name == "Windows 系统代理")
			return content, nil
		}
		failures = append(failures, fmt.Sprintf("%s: %v", route.name, err))
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, fmt.Errorf("在线音源请求失败；已尝试：%s", strings.Join(failures, "；"))
}

func onlineBytesWithClient(ctx context.Context, base *http.Client, rawURL, host string, limit int64) ([]byte, error) {
	return onlineBytesWithClientProgress(ctx, base, rawURL, host, limit, nil)
}

func onlineBytesWithClientProgress(ctx context.Context, base *http.Client, rawURL, host string, limit int64, report func(int64)) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || u.Hostname() != host {
		return nil, fmt.Errorf("音源地址不在已支持的网站")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", "yovoice-instrument-discovery/0.2 (CC0 verification)")
	client := *base
	if client.Timeout == 0 {
		client.Timeout = 25 * time.Second
	}
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || req.URL.Scheme != "https" || req.URL.Hostname() != host {
			return fmt.Errorf("音源下载跳转到未核验的网站")
		}
		return nil
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK || response.Request.URL.Hostname() != host {
		return nil, fmt.Errorf("音源网站返回 HTTP %d", response.StatusCode)
	}
	if response.ContentLength > limit {
		return nil, fmt.Errorf("在线音源超过大小限制")
	}
	var content bytes.Buffer
	var last int64
	writer := &progressWriter{ctx: ctx, out: &content, report: func(received int64) {
		if report != nil && (received-last >= 256<<10 || received == response.ContentLength) {
			last = received
			report(received)
		}
	}}
	received, err := io.Copy(writer, io.LimitReader(response.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if received > limit {
		return nil, fmt.Errorf("在线音源超过大小限制")
	}
	if report != nil {
		report(received)
	}
	return content.Bytes(), nil
}

// SearchInstrumentCandidates 只查 CC0 聚合目录；搜索结果是待核验线索，不等同于可用乐器。
func (w *Workbench) SearchInstrumentCandidates(ctx context.Context, instrument string) ([]InstrumentCandidate, error) {
	candidates, _, err := w.searchInstrumentPage(ctx, instrument, 1, false)
	return candidates, err
}

// 自动搜索仅扫描前三页且限定 Freesound，避免把聚合搜索当作批量抓取接口。
func (w *Workbench) searchInstrumentPage(ctx context.Context, instrument string, page int, freesoundOnly bool) ([]InstrumentCandidate, bool, error) {
	term := onlineSearchTerm(instrument)
	if term == "" || schema.TextLen(term) > 80 || page < 1 || page > 3 {
		return nil, false, msg.Err(msg.ErrRequestInvalid, nil)
	}
	query := url.Values{"q": {term}, "license": {"cc0"}, "page_size": {"20"}, "page": {fmt.Sprint(page)}}
	if freesoundOnly {
		query.Set("source", "freesound")
	}
	content, err := w.onlineBytes(ctx, openverseAudioURL+"?"+query.Encode(), "api.openverse.org", 2<<20)
	if err != nil {
		return nil, false, err
	}
	var response struct {
		Results   []openverseAudio `json:"results"`
		PageCount int              `json:"page_count"`
	}
	if err := json.Unmarshal(content, &response); err != nil {
		return nil, false, err
	}
	candidates := make([]InstrumentCandidate, 0, len(response.Results))
	for _, item := range response.Results {
		if candidate, ok := item.candidate(); ok {
			candidates = append(candidates, candidate)
		}
	}
	return candidates, page < response.PageCount, nil
}

func (w *Workbench) onlineCandidate(ctx context.Context, id string) (openverseAudio, error) {
	if !openverseID.MatchString(id) {
		return openverseAudio{}, msg.Err(msg.ErrRequestInvalid, nil)
	}
	content, err := w.onlineBytes(ctx, openverseAudioURL+id+"/", "api.openverse.org", 2<<20)
	if err != nil {
		return openverseAudio{}, err
	}
	var item openverseAudio
	if err := json.Unmarshal(content, &item); err != nil {
		return openverseAudio{}, err
	}
	if item.ID != id {
		return openverseAudio{}, fmt.Errorf("在线音源标识与详情不一致")
	}
	if _, ok := item.candidate(); !ok {
		return openverseAudio{}, fmt.Errorf("在线音源不满足 CC0 和文件限制")
	}
	return item, nil
}

func (w *Workbench) verifiedMediaURL(ctx context.Context, item openverseAudio) (string, error) {
	if item.Source == "freesound" {
		page, err := w.onlineBytes(ctx, item.LandingURL, "freesound.org", 2<<20)
		if err != nil {
			return "", err
		}
		doc, err := html.Parse(bytes.NewReader(page))
		if err != nil {
			return "", err
		}
		var cc0 bool
		var visit func(*html.Node)
		visit = func(node *html.Node) {
			if node.Type == html.ElementNode && node.Data == "a" {
				var licenseLink, licenseTitle bool
				for _, attr := range node.Attr {
					if attr.Key == "href" && (strings.HasPrefix(attr.Val, "http://creativecommons.org/publicdomain/zero/1.0/") || strings.HasPrefix(attr.Val, "https://creativecommons.org/publicdomain/zero/1.0/")) {
						licenseLink = true
					}
					if attr.Key == "title" && attr.Val == "Go to the full license text" {
						licenseTitle = true
					}
				}
				cc0 = cc0 || licenseLink && licenseTitle
			}
			for child := node.FirstChild; child != nil; child = child.NextSibling {
				visit(child)
			}
		}
		visit(doc)
		if !cc0 {
			return "", fmt.Errorf("原站未确认 CC0 许可")
		}
		return item.URL, nil
	}
	landing, _ := url.Parse(item.LandingURL)
	pageID := landing.Query().Get("curid")
	if !numericPageID.MatchString(pageID) {
		return "", fmt.Errorf("维基共享资源页面标识无效")
	}
	api := "https://commons.wikimedia.org/w/api.php?action=query&prop=imageinfo&iiprop=url%7Cextmetadata&pageids=" + pageID + "&format=json"
	content, err := w.onlineBytes(ctx, api, "commons.wikimedia.org", 2<<20)
	if err != nil {
		return "", err
	}
	var response struct {
		Query struct {
			Pages map[string]struct {
				ImageInfo []struct {
					URL         string `json:"url"`
					ExtMetadata map[string]struct {
						Value string `json:"value"`
					} `json:"extmetadata"`
				} `json:"imageinfo"`
			} `json:"pages"`
		} `json:"query"`
	}
	if err := json.Unmarshal(content, &response); err != nil {
		return "", err
	}
	for _, page := range response.Query.Pages {
		if len(page.ImageInfo) == 0 {
			continue
		}
		info := page.ImageInfo[0]
		if info.ExtMetadata["LicenseShortName"].Value != "CC0" ||
			strings.Contains(strings.ToLower(info.ExtMetadata["Categories"].Value), "pronunciation") {
			return "", fmt.Errorf("原站许可或内容类型不满足单音乐器要求")
		}
		media, err := url.Parse(info.URL)
		if err == nil && media.Scheme == "https" && media.Hostname() == "upload.wikimedia.org" && media.String() == item.URL {
			return info.URL, nil
		}
	}
	return "", fmt.Errorf("原站文件地址与聚合目录不一致，或未确认 CC0 许可")
}

// ImportOnlineInstrumentSample 重新查原站许可和实际声音，只有稳定单音才进入可按音符调用的库。
func (w *Workbench) ImportOnlineInstrumentSample(ctx context.Context, in OnlineSampleInput) (schema.InstrumentSource, error) {
	if !in.ConfirmedInstrument {
		return schema.InstrumentSource{}, msg.Err(msg.ErrRequestInvalid, nil)
	}
	return w.importOnlineInstrumentSample(ctx, in, "user")
}

func (w *Workbench) importOnlineInstrumentSample(ctx context.Context, in OnlineSampleInput, verification string) (schema.InstrumentSource, error) {
	if schema.TextLen(strings.TrimSpace(in.Instrument)) < 1 || schema.TextLen(in.Instrument) > 80 || in.Articulation != "normal" || !openverseID.MatchString(in.ID) || (verification != "user" && verification != "metadata") {
		return schema.InstrumentSource{}, msg.Err(msg.ErrRequestInvalid, nil)
	}
	item, err := w.onlineCandidate(ctx, in.ID)
	if err != nil {
		return schema.InstrumentSource{}, err
	}
	if verification == "metadata" && !autoTitleMatches(item, in.Instrument) {
		return schema.InstrumentSource{}, fmt.Errorf("在线标题不能确认是 %s 乐器", in.Instrument)
	}
	mediaURL, err := w.verifiedMediaURL(ctx, item)
	if err != nil {
		return schema.InstrumentSource{}, err
	}
	host := "cdn.freesound.org"
	if item.Source == "wikimedia_audio" {
		host = "upload.wikimedia.org"
	}
	content, err := w.onlineBytes(ctx, mediaURL, host, maxUpload)
	if err != nil {
		return schema.InstrumentSource{}, err
	}
	stage, err := os.MkdirTemp(filepath.Join(w.Store.Root, "downloads"), "candidate-*")
	if err != nil {
		return schema.InstrumentSource{}, err
	}
	defer os.RemoveAll(stage)
	original := filepath.Join(stage, "original."+strings.ToLower(item.FileType))
	normalized := filepath.Join(stage, "sample.wav")
	if err := os.WriteFile(original, content, 0600); err != nil {
		return schema.InstrumentSource{}, err
	}
	if _, err := score.InspectSample(original); err == nil {
		normalized = original
	} else {
		converter, err := audio.Converter()
		if err != nil {
			return schema.InstrumentSource{}, err
		}
		if err := audio.ConvertInstrument(ctx, converter, original, normalized); err != nil {
			return schema.InstrumentSource{}, err
		}
	}
	pitch, _, err := score.EstimateRootPitch(normalized)
	if err != nil {
		return schema.InstrumentSource{}, err
	}
	file, err := os.Open(normalized)
	if err != nil {
		return schema.InstrumentSource{}, err
	}
	defer file.Close()
	return w.ImportInstrumentSampleFrom(ctx, file, InstrumentSampleInput{SourceID: in.SourceID, Name: item.Title, Instrument: in.Instrument,
		Origin: "real", Verification: verification, Author: item.Creator, License: "CC0-1.0", LicenseURL: "https://creativecommons.org/publicdomain/zero/1.0/",
		SourceURL: item.LandingURL, RootPitch: pitch, Articulation: "normal", VelocityLow: 1, VelocityHigh: 127})
}
