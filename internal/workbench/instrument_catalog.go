package workbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
)

// 固定到已审核的 CC0 提交，运行时不接收模型返回的任意下载地址。
const xsxbCommit = "4ce222ecff5f6f1e38470b1e8b22eb31b1a3a040"
const xsxbRaw = "https://raw.githubusercontent.com/sparklecatta-lang/XSXB-Band-Samples/" + xsxbCommit + "/"

var githubRawSegment = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
var githubPinnedCommit = regexp.MustCompile(`^[0-9a-f]{40}$`)

// raw.githubusercontent.com 在部分网络下连接会被重置；仅对固定提交的 GitHub
// 文件改试 jsDelivr。调用方仍需按清单中的 SHA-256 核验采样内容。
func githubRawMirrorURL(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() != "raw.githubusercontent.com" || u.RawQuery != "" || u.Fragment != "" ||
		strings.Contains(strings.ToLower(u.EscapedPath()), "%2f") || strings.Contains(strings.ToLower(u.EscapedPath()), "%5c") {
		return "", false
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) < 4 || !githubRawSegment.MatchString(parts[0]) || !githubRawSegment.MatchString(parts[1]) || !githubPinnedCommit.MatchString(parts[2]) {
		return "", false
	}
	for i := 3; i < len(parts); i++ {
		if parts[i] == "" || parts[i] == "." || parts[i] == ".." || strings.Contains(parts[i], `\`) {
			return "", false
		}
		parts[i] = url.PathEscape(parts[i])
	}
	return "https://cdn.jsdelivr.net/gh/" + parts[0] + "/" + parts[1] + "@" + parts[2] + "/" + strings.Join(parts[3:], "/"), true
}

func (w *Workbench) catalogSampleBytes(ctx context.Context, raw string, maxBytes int64) ([]byte, error) {
	return w.catalogSampleBytesWithFallback(ctx, raw, maxBytes, catalogSystemProxy())
}

// catalogSampleBytesVerified uses a deterministic cache for fixed catalog
// entries.  The cache lives below downloads/ so it is outside the media
// library until the sample has passed the catalog hash and format checks.
// This makes a successful download reusable even when the network is later
// unavailable, and prevents a partially downloaded file from becoming an
// instrument by accident.
func (w *Workbench) catalogSampleBytesVerified(ctx context.Context, raw string, maxBytes, expectedBytes int64, expectedHash string) ([]byte, error) {
	return w.catalogSampleBytesVerifiedWithProgress(ctx, raw, maxBytes, expectedBytes, expectedHash, nil)
}

func (w *Workbench) catalogSampleBytesVerifiedWithProgress(ctx context.Context, raw string, maxBytes, expectedBytes int64, expectedHash string, report func(int64)) ([]byte, error) {
	key := strings.ToLower(strings.TrimSpace(expectedHash))
	if !xsxbSampleSHA.MatchString(key) {
		sum := sha256.Sum256([]byte(raw))
		key = hex.EncodeToString(sum[:])
	}
	cacheDir := filepath.Join(w.Store.Root, "downloads", "instrument-cache")
	cachePath := filepath.Join(cacheDir, key+".bin")
	if b, err := os.ReadFile(cachePath); err == nil {
		if int64(len(b)) == expectedBytes {
			sum := sha256.Sum256(b)
			if expectedHash == "" || strings.EqualFold(hex.EncodeToString(sum[:]), expectedHash) {
				if report != nil {
					report(int64(len(b)))
				}
				return b, nil
			}
		}
		_ = os.Remove(cachePath)
	}
	b, err := w.catalogSampleBytesWithFallbackProgress(ctx, raw, maxBytes, catalogSystemProxy(), report)
	if err != nil {
		return nil, err
	}
	if expectedBytes > 0 && int64(len(b)) != expectedBytes {
		return nil, fmt.Errorf("音源缓存前大小校验失败")
	}
	if expectedHash != "" {
		sum := sha256.Sum256(b)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), expectedHash) {
			return nil, fmt.Errorf("音源缓存前 SHA-256 校验失败")
		}
	}
	if err := os.MkdirAll(cacheDir, 0700); err != nil {
		return nil, err
	}
	partial := cachePath + ".part"
	if err := os.WriteFile(partial, b, 0600); err != nil {
		return nil, err
	}
	if err := os.Rename(partial, cachePath); err != nil {
		_ = os.Remove(partial)
		return nil, err
	}
	return b, nil
}

type catalogNetworkRoute struct {
	name     string
	proxy    *url.URL
	envProxy bool
}

const (
	catalogRouteConfigured  int32 = 1
	catalogRouteSystem      int32 = 2
	catalogRouteEnvironment int32 = 3
	catalogRouteDirect      int32 = 4
)

func catalogProxySame(a, b *url.URL) bool {
	if a == nil || b == nil {
		return a == b
	}
	return strings.EqualFold(strings.TrimRight(a.String(), "/"), strings.TrimRight(b.String(), "/"))
}

func catalogEnvironmentProxy() *url.URL {
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "HTTP_PROXY", "http_proxy"} {
		if proxy := catalogParseProxy(os.Getenv(key)); proxy != nil {
			return proxy
		}
	}
	return nil
}

func (w *Workbench) catalogRoutes(systemProxy *url.URL) []catalogNetworkRoute {
	preferences := w.Store.Read().Preferences
	configured := preferences.ProxyURL != ""
	if preferences.ProxyEnabled != nil {
		configured = *preferences.ProxyEnabled
	}
	routes := make([]catalogNetworkRoute, 0, 4)
	if configured {
		if proxy := catalogParseProxy(preferences.ProxyURL); proxy != nil {
			routes = append(routes, catalogNetworkRoute{name: "应用代理", proxy: proxy})
		}
	}
	if systemProxy != nil && !catalogProxySame(systemProxy, catalogProxyFromPreferences(preferences, configured)) {
		routes = append(routes, catalogNetworkRoute{name: "Windows 系统代理", proxy: systemProxy})
	}
	if envProxy := catalogEnvironmentProxy(); envProxy != nil && !catalogProxySame(envProxy, systemProxy) && !catalogProxySame(envProxy, catalogProxyFromPreferences(preferences, configured)) {
		routes = append(routes, catalogNetworkRoute{name: "环境代理", proxy: envProxy, envProxy: true})
	}
	routes = append(routes, catalogNetworkRoute{name: "直连"})
	return routes
}

func catalogProxyFromPreferences(preferences schema.Preferences, enabled bool) *url.URL {
	if !enabled {
		return nil
	}
	return catalogParseProxy(preferences.ProxyURL)
}

func (w *Workbench) catalogClient(route catalogNetworkRoute) (*http.Client, error) {
	base, ok := w.client.Transport.(*http.Transport)
	if !ok {
		if route.proxy == nil && !route.envProxy {
			client := *w.client
			if client.Timeout == 0 {
				client.Timeout = 20 * time.Second
			}
			return &client, nil
		}
		return nil, fmt.Errorf("当前网络客户端不支持%s", route.name)
	}
	transport := base.Clone()
	// 不继承应用客户端上的旧代理。目录下载每次明确选择一条路径。
	transport.Proxy = nil
	if route.envProxy {
		transport.Proxy = http.ProxyFromEnvironment
	} else if route.proxy != nil {
		transport.Proxy = http.ProxyURL(route.proxy)
	}
	// 大型 WAV 的传输可超过短请求超时；连接与响应头仍尽早失败。
	transport.ResponseHeaderTimeout = 12 * time.Second
	return &http.Client{Transport: transport, Timeout: 50 * time.Second}, nil
}

func (w *Workbench) catalogRouteOrder(routes []catalogNetworkRoute) []catalogNetworkRoute {
	preferred := w.catalogPreferredRoute.Load()
	if preferred < 1 || int(preferred) > len(routes) {
		return routes
	}
	ordered := make([]catalogNetworkRoute, 0, len(routes))
	ordered = append(ordered, routes[preferred-1])
	ordered = append(ordered, routes[:preferred-1]...)
	ordered = append(ordered, routes[preferred:]...)
	return ordered
}

func (w *Workbench) catalogSampleBytesWithFallback(ctx context.Context, raw string, maxBytes int64, fallback *url.URL) ([]byte, error) {
	return w.catalogSampleBytesWithFallbackProgress(ctx, raw, maxBytes, fallback, nil)
}

func (w *Workbench) catalogSampleBytesWithFallbackProgress(ctx context.Context, raw string, maxBytes int64, fallback *url.URL, report func(int64)) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !slices.Contains([]string{"raw.githubusercontent.com", "cdn.freesound.org"}, u.Hostname()) {
		return nil, fmt.Errorf("音源地址不在已支持的网站")
	}
	mirror, hasMirror := githubRawMirrorURL(raw)
	routes := w.catalogRouteOrder(w.catalogRoutes(fallback))
	var failures []string
	for index, route := range routes {
		client, clientErr := w.catalogClient(route)
		if clientErr != nil {
			failures = append(failures, route.name+": "+clientErr.Error())
			continue
		}
		endpoints := []struct{ url, host, name string }{{raw, u.Hostname(), "GitHub 原站"}}
		if hasMirror {
			mirrorEndpoint := struct{ url, host, name string }{mirror, "cdn.jsdelivr.net", "jsDelivr 镜像"}
			if w.catalogMirrorPreferred.Load() {
				endpoints = []struct{ url, host, name string }{mirrorEndpoint, endpoints[0]}
			} else {
				endpoints = append(endpoints, mirrorEndpoint)
			}
		}
		for _, endpoint := range endpoints {
			attemptCtx, cancel := context.WithTimeout(ctx, 45*time.Second)
			if report != nil {
				report(0)
			}
			b, requestErr := onlineBytesWithClientProgress(attemptCtx, client, endpoint.url, endpoint.host, maxBytes, report)
			cancel()
			if requestErr == nil {
				w.catalogPreferredRoute.Store(int32(index + 1))
				w.catalogSystemProxyPreferred.Store(route.name == "Windows 系统代理")
				w.catalogMirrorPreferred.Store(endpoint.name == "jsDelivr 镜像")
				return b, nil
			}
			failures = append(failures, fmt.Sprintf("%s/%s: %v", route.name, endpoint.name, requestErr))
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
		}
	}
	return nil, fmt.Errorf("无法下载音源；已尝试：%s", strings.Join(failures, "；"))
}

type catalogInstrument struct{ ID, Name, Instrument, Description string }

var instrumentCatalog = []catalogInstrument{
	{ID: "guzheng", Name: "古筝 · XSXB CC0", Instrument: "古筝", Description: "真实古筝录音；来源为 Freesound Pufermufin 396868，CC0。"},
	{ID: "xiao", Name: "箫音色 · 竖笛采样模拟", Instrument: "箫（模拟）", Description: "VCSL 竖笛录音加工；不是箫原声。"},
	{ID: "suona-oboe", Name: "唢呐音色 · 双簧管采样模拟", Instrument: "唢呐（模拟）", Description: "VSCO 2 CE 双簧管录音加工；不是唢呐原声。"},
}

type xsxbManifest struct {
	License     string `json:"license"`
	Instruments []struct {
		ID                  string `json:"id"`
		License             string `json:"license"`
		Author              string `json:"author"`
		DefaultArticulation string `json:"defaultArticulation"`
		UpstreamSource      string `json:"upstreamSource"`
		Samples             []struct {
			MIDI         int    `json:"midi"`
			Articulation string `json:"articulation"`
			File         string `json:"file"`
		} `json:"samples"`
	} `json:"instruments"`
}

func (w *Workbench) catalogBytes(ctx context.Context, path string, maxBytes int64) ([]byte, error) {
	return w.catalogSampleBytes(ctx, xsxbRaw+path, maxBytes)
}

// DownloadCatalogInstrument 在后台按需下载一件乐器，全部采样通过核验后才入库。
func (w *Workbench) DownloadCatalogInstrument(id string) error {
	i := slices.IndexFunc(instrumentCatalog, func(item catalogInstrument) bool { return item.ID == id })
	if i < 0 {
		return w.downloadXSXBLibraryInstrument(id)
	}
	for _, source := range w.Store.Read().InstrumentSources {
		if source.CatalogID == id && w.sourceFilesIssue(source) == "" {
			return nil
		}
	}
	item := instrumentCatalog[i]
	return w.begin("instrumentDownload", msg.ActivityDownload, msg.Params{"name": item.Name}, nil, func(ctx context.Context) error {
		manifestBytes, err := w.catalogBytes(ctx, "instrument.json", 4<<20)
		if err != nil {
			return err
		}
		var manifest xsxbManifest
		if err = json.Unmarshal(manifestBytes, &manifest); err != nil {
			return err
		}
		if manifest.License != "CC0-1.0" {
			return fmt.Errorf("音源库的许可已变化，停止下载")
		}
		index := slices.IndexFunc(manifest.Instruments, func(v struct {
			ID                  string `json:"id"`
			License             string `json:"license"`
			Author              string `json:"author"`
			DefaultArticulation string `json:"defaultArticulation"`
			UpstreamSource      string `json:"upstreamSource"`
			Samples             []struct {
				MIDI         int    `json:"midi"`
				Articulation string `json:"articulation"`
				File         string `json:"file"`
			} `json:"samples"`
		}) bool {
			return v.ID == id
		})
		if index < 0 {
			return fmt.Errorf("音源清单缺少 %s", id)
		}
		remote := manifest.Instruments[index]
		if remote.License != "CC0-1.0" || len(remote.Samples) < 1 || len(remote.Samples) > 150 || remote.DefaultArticulation == "" {
			return fmt.Errorf("音源清单不符合审核条件")
		}
		w.progressOf("instrumentDownload", msg.ActivityDownloading, msg.Params{"name": item.Name}, 0, int64(len(remote.Samples)))
		converter, err := audio.Converter()
		if err != nil {
			return err
		}
		stage, err := os.MkdirTemp(filepath.Join(w.Store.Root, "downloads"), "xsxb-*")
		if err != nil {
			return err
		}
		defer os.RemoveAll(stage)
		source := schema.InstrumentSource{ID: schema.NewID(), CatalogID: id, Name: item.Name, Instrument: item.Instrument, Origin: "real", Verification: "catalog", DefaultArticulation: remote.DefaultArticulation, Author: remote.Author, License: "CC0-1.0", LicenseURL: "https://creativecommons.org/publicdomain/zero/1.0/", SourceURL: "https://github.com/sparklecatta-lang/XSXB-Band-Samples/tree/" + xsxbCommit, Samples: []schema.InstrumentSample{}}
		var total int64
		for i, sample := range remote.Samples {
			if err := ctx.Err(); err != nil {
				return err
			}
			if sample.MIDI < 0 || sample.MIDI > 127 || sample.Articulation == "" || len(sample.Articulation) > 40 || !strings.HasPrefix(sample.File, "samples/"+id+"/") || strings.Contains(sample.File, "..") || !strings.HasSuffix(sample.File, ".flac") {
				return fmt.Errorf("音源清单含无效采样路径")
			}
			b, err := w.catalogBytes(ctx, sample.File, 20<<20)
			if err != nil {
				return err
			}
			total += int64(len(b))
			if total > 200<<20 {
				return fmt.Errorf("音源总大小超出限制")
			}
			original := filepath.Join(stage, fmt.Sprintf("%03d.flac", i))
			converted := filepath.Join(stage, fmt.Sprintf("%03d.wav", i))
			if err = os.WriteFile(original, b, 0600); err != nil {
				return err
			}
			if err = audio.ConvertInstrumentWithLimit(ctx, converter, original, converted, catalogSampleMaxSeconds); err != nil {
				return err
			}
			_ = os.Remove(original)
			duration, err := audio.Duration(converted)
			if err != nil || duration < .05 || duration > catalogSampleMaxSeconds {
				return fmt.Errorf("音源采样时长不合规: %s", sample.File)
			}
			if _, err = score.InspectSample(converted); err != nil {
				return err
			}
			data, err := os.ReadFile(converted)
			if err != nil {
				return err
			}
			hash := sha256.Sum256(data)
			sampleID := schema.NewID()
			source.Samples = append(source.Samples, schema.InstrumentSample{ID: sampleID, FileName: "sample-" + sampleID + ".wav", SHA256: hex.EncodeToString(hash[:]), RootPitch: sample.MIDI, Articulation: sample.Articulation, VelocityLow: 1, VelocityHigh: 127, Duration: duration})
			w.progressOf("instrumentDownload", msg.ActivityDownloading, msg.Params{"name": item.Name}, int64(i+1), int64(len(remote.Samples)))
		}
		w.edit.Lock()
		defer w.edit.Unlock()
		for _, existing := range w.Store.Read().InstrumentSources {
			if existing.CatalogID == id {
				if w.sourceFilesIssue(existing) == "" {
					return nil
				}
				source.ID = existing.ID
			}
		}
		written := []string{}
		keep := false
		defer func() {
			if !keep {
				for _, path := range written {
					_ = os.Remove(path)
				}
			}
		}()
		for i, sample := range source.Samples {
			dest, err := w.Store.MediaPath("instruments", sample.FileName)
			if err != nil {
				return err
			}
			if err = os.Rename(filepath.Join(stage, fmt.Sprintf("%03d.wav", i)), dest); err != nil {
				return err
			}
			written = append(written, dest)
		}
		if err = w.Store.Update(func(s *schema.State) {
			for i := range s.InstrumentSources {
				if s.InstrumentSources[i].ID == source.ID {
					s.InstrumentSources[i] = source
					return
				}
			}
			s.InstrumentSources = append(s.InstrumentSources, source)
		}, true); err != nil {
			return err
		}
		keep = true
		return nil
	})
}
