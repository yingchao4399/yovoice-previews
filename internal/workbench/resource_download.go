package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"yovoice/internal/msg"
	"yovoice/internal/schema"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type ResourceDownloadInput struct {
	ProjectID  string   `json:"projectId,omitempty"`
	CatalogIDs []string `json:"catalogIds" jsonschema:"search_resources返回的精确catalogId，最多8个；不能传URL"`
	Reason     string   `json:"reason,omitempty"`
}

func (w *Workbench) resourceDownloadSpec(id string) (schema.ResourceDownloadSpec, error) {
	r, e := w.ResolveResource("catalog:" + id)
	if e != nil || r.Kind != "catalog" || r.License != "CC0-1.0" || r.Manifest != xsxbLibrarySHA256 || r.Bytes < 1 || r.Bytes > 200<<20 {
		return schema.ResourceDownloadSpec{}, fmt.Errorf("该音源不是可确认下载的固定CC0目录条目：%s", id)
	}
	state := "queued"
	if r.Status == "installed" {
		state = "completed"
	}
	return schema.ResourceDownloadSpec{CatalogID: id, Name: r.Name, License: r.License, Bytes: r.Bytes, Manifest: r.Manifest, State: state}, nil
}

func cloneDownloadProposal(p schema.AssistantProposal) schema.AssistantProposal {
	b, _ := json.Marshal(p)
	var copy schema.AssistantProposal
	_ = json.Unmarshal(b, &copy)
	return copy
}

func (w *Workbench) resourceDownloadProposals(projectID string) []schema.AssistantProposal {
	var out []schema.AssistantProposal
	if sessions := w.Store.Read().AssistantSessions; sessions != nil {
		for _, session := range sessions.Sessions {
			for _, message := range session.Messages {
				for _, block := range message.Blocks {
					if block.Card != nil && block.Card.Proposal != nil && block.Card.Proposal.Downloads != nil {
						p := block.Card.Proposal
						if projectID == "" || p.Downloads.ProjectID == projectID {
							out = append(out, cloneDownloadProposal(*p))
						}
					}
				}
			}
		}
	}
	return out
}

func (w *Workbench) registerResourceDownloadTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "propose_resource_download", Description: "为精确的CC0目录ID生成下载确认卡，显示许可、大小和清单。先查本地，合适的健康本地采样不下载。只有用户点确认才能下载；普通问答和confirmed参数无效。返回awaiting_approval后结束本轮，完成后会通知原会话。"}, func(ctx context.Context, req *mcp.CallToolRequest, in ResourceDownloadInput) (*mcp.CallToolResult, any, error) {
		p, e := w.ProposeResourceDownloads(in)
		if e != nil {
			return nil, nil, e
		}
		return nil, map[string]any{"state": "awaiting_approval", "proposal": p}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "resource_downloads", Description: "查询已提议的音源下载清单、确认状态和逐项完成情况。queued/running不等于已完成；失败或暂停不能宣称全部成功。"}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		ProjectID string `json:"projectId,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		return nil, w.resourceDownloadProposals(in.ProjectID), nil
	})
}

func (w *Workbench) ProposeResourceDownloads(in ResourceDownloadInput) (schema.AssistantProposal, error) {
	if len(in.CatalogIDs) < 1 || len(in.CatalogIDs) > 8 || schema.TextLen(in.Reason) > 400 {
		return schema.AssistantProposal{}, fmt.Errorf("每批需选择1至8个已审核目录音源")
	}
	w.chat.mu.Lock()
	turn := w.chat.turn
	sessionID := ""
	if turn != nil {
		sessionID = turn.sessionID
	}
	w.chat.mu.Unlock()
	if turn == nil || sessionID == "" {
		return schema.AssistantProposal{}, fmt.Errorf("没有发起下载提议的原会话")
	}
	batch := &schema.ResourceDownloadBatch{SessionID: sessionID, ProjectID: in.ProjectID, State: "awaiting_approval"}
	if in.ProjectID != "" {
		d, e := w.assistantWork(in.ProjectID)
		if e != nil {
			return schema.AssistantProposal{}, e
		}
		if resourcePolicyForDraft(d) == "ai" {
			return schema.AssistantProposal{}, fmt.Errorf("当前工程仅允许AI音源，不能自动请求真实采样下载")
		}
		batch.Revision = arrangementRevision(d)
	}
	seen := map[string]bool{}
	names := []string{}
	var total int64
	for _, id := range in.CatalogIDs {
		if seen[id] {
			return schema.AssistantProposal{}, fmt.Errorf("清单包含重复音源")
		}
		seen[id] = true
		spec, e := w.resourceDownloadSpec(id)
		if e != nil {
			return schema.AssistantProposal{}, e
		}
		if spec.State == "completed" {
			continue
		}
		batch.Items = append(batch.Items, spec)
		names = append(names, spec.Name)
		total += spec.Bytes
	}
	if len(batch.Items) == 0 {
		return schema.AssistantProposal{}, fmt.Errorf("所选音源已健康地保存在本地，无需下载，请重新查询库存")
	}
	p := schema.AssistantProposal{ID: schema.NewID(), Action: "download_resources", State: "pending", Reason: strings.TrimSpace(in.Reason), Label: fmt.Sprintf("%s · CC0-1.0 · %.1f MB", strings.Join(names, "、"), float64(total)/1e6), Downloads: batch}
	found := false
	err := w.Store.Update(func(s *schema.State) {
		session := assistantSessionByID(*s, sessionID)
		if session == nil {
			return
		}
		found = true
		session.Messages = append(session.Messages, schema.AssistantMessage{ID: schema.NewID(), Role: "assistant", Status: "completed", CreatedAt: time.Now().UTC(), Blocks: []schema.AssistantBlock{{Kind: "tool", Card: &schema.AssistantToolCard{Tool: "propose_resource_download", Summary: p.Label, State: "done", Proposal: &p}}}})
	}, true)
	if err != nil {
		return schema.AssistantProposal{}, err
	}
	if !found {
		return schema.AssistantProposal{}, fmt.Errorf("原会话已关闭")
	}
	w.chat.mu.Lock()
	if w.chat.turn == turn {
		turn.waiting = true
	}
	w.chat.mu.Unlock()
	time.AfterFunc(350*time.Millisecond, func() {
		w.chat.mu.Lock()
		same := w.chat.turn == turn
		yield := turn.yield
		w.chat.mu.Unlock()
		if same && yield != nil {
			yield()
		}
	})
	return p, nil
}

func (w *Workbench) validateResourceApproval(p schema.AssistantProposal) error {
	if p.Action != "download_resources" || p.Downloads == nil || len(p.Downloads.Items) < 1 || len(p.Downloads.Items) > 8 {
		return fmt.Errorf("下载确认清单无效")
	}
	if p.Downloads.ProjectID != "" {
		d, e := w.assistantWork(p.Downloads.ProjectID)
		if e != nil {
			return e
		}
		if arrangementRevision(d) != p.Downloads.Revision {
			return fmt.Errorf("编曲已变化，请重新盘点和确认下载清单")
		}
	}
	for _, item := range p.Downloads.Items {
		current, e := w.resourceDownloadSpec(item.CatalogID)
		if e != nil {
			return e
		}
		if current.Manifest != item.Manifest || current.Name != item.Name || current.Bytes != item.Bytes || current.License != item.License {
			return fmt.Errorf("目录或许可已变化，需要重新确认")
		}
	}
	return nil
}

func (w *Workbench) resolveResourceDownloadProposal(id string, confirm bool) error {
	w.resourceEdit.Lock()
	defer w.resourceEdit.Unlock()
	p := w.findProposal(id)
	if p == nil || p.Downloads == nil {
		return fmt.Errorf("没有找到下载清单")
	}
	if p.State == "confirmed" && confirm {
		return nil
	}
	if p.State != "pending" {
		return fmt.Errorf("该清单已处理")
	}
	next := cloneDownloadProposal(*p)
	if !confirm {
		next.State = "dismissed"
		next.Downloads.State = "cancelled"
		return w.storeProposal(next)
	}
	if e := w.validateResourceApproval(next); e != nil {
		return e
	}
	next.State = "confirmed"
	next.Downloads.State = "queued"
	if e := w.storeProposal(next); e != nil {
		return e
	}
	w.startResourceDownloadWorker()
	return nil
}

func (w *Workbench) changeResourceDownload(id string, change func(*schema.AssistantProposal)) error {
	w.resourceEdit.Lock()
	defer w.resourceEdit.Unlock()
	p := w.findProposal(id)
	if p == nil || p.Downloads == nil {
		return fmt.Errorf("下载清单已删除")
	}
	next := cloneDownloadProposal(*p)
	change(&next)
	return w.storeProposal(next)
}

// 只有服务端记录为confirmed的固定清单进入队列；每项成功立即保留，失败不会撤回前面的音源。
func (w *Workbench) startResourceDownloadWorker() {
	proposals := w.resourceDownloadProposals("")
	if !slices.ContainsFunc(proposals, func(p schema.AssistantProposal) bool {
		return p.State == "confirmed" && (p.Downloads.State == "queued" || p.Downloads.State == "running" || p.Downloads.State == "completed" && !p.Downloads.Notified)
	}) {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed || w.resourceCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	w.resourceCancel = cancel
	w.resourceDone = make(chan struct{})
	done := w.resourceDone
	go func() {
		defer close(done)
		changes, stop := w.Store.Subscribe()
		defer stop()
		for {
			if ctx.Err() != nil {
				return
			}
			var next *schema.AssistantProposal
			for _, p := range w.resourceDownloadProposals("") {
				if p.State == "confirmed" && p.Downloads.State == "completed" && !p.Downloads.Notified {
					w.notifyResourceDownloads(p.ID)
				}
				if p.State == "confirmed" && (p.Downloads.State == "queued" || p.Downloads.State == "running") {
					copy := p
					next = &copy
					break
				}
			}
			if next == nil {
				select {
				case <-ctx.Done():
					return
				case <-changes:
				}
				continue
			}
			if e := w.validateResourceApproval(*next); e != nil {
				_ = w.changeResourceDownload(next.ID, func(p *schema.AssistantProposal) {
					p.Downloads.State = "failed"
					p.ErrorCode = msg.ErrUnknown
					p.ErrorParams = msg.Params{"detail": e.Error()}
				})
				continue
			}
			jobID := next.ID
			finished, e := w.start("instrumentDownload", msg.ActivityDownload, msg.Params{"name": next.Label}, nil, func(downloadCtx context.Context) error { return w.runResourceDownloadBatch(downloadCtx, jobID) }, jobID)
			if e != nil {
				select {
				case <-ctx.Done():
					return
				case <-changes:
				}
				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-finished:
			}
		}
	}()
}

func (w *Workbench) runResourceDownloadBatch(ctx context.Context, id string) error {
	if e := w.changeResourceDownload(id, func(p *schema.AssistantProposal) { p.Downloads.State = "running" }); e != nil {
		return e
	}
	p := w.findProposal(id)
	if p == nil {
		return fmt.Errorf("下载清单已删除")
	}
	for index, item := range p.Downloads.Items {
		current := w.findProposal(id)
		if current == nil || current.State != "confirmed" || current.Downloads.State == "cancelled" {
			return context.Canceled
		}
		if e := w.validateResourceApproval(*current); e != nil {
			_ = w.changeResourceDownload(id, func(p *schema.AssistantProposal) {
				p.Downloads.State = "failed"
				p.ErrorCode = msg.ErrUnknown
				p.ErrorParams = msg.Params{"detail": e.Error()}
			})
			return e
		}
		_ = w.changeResourceDownload(id, func(p *schema.AssistantProposal) { p.Downloads.Items[index].State = "running" })
		err := w.installXSXBLibraryInstrument(ctx, item.CatalogID, xsxbLibraryJSON, item.Manifest)
		if err != nil {
			w.mu.Lock()
			closing := w.closed
			w.mu.Unlock()
			_ = w.changeResourceDownload(id, func(p *schema.AssistantProposal) {
				if p.Downloads.State == "cancelled" {
					return
				}
				p.Downloads.Items[index].State = "failed"
				p.Downloads.State = "failed"
				if errors.Is(err, context.Canceled) {
					p.Downloads.State = "paused"
					if closing {
						p.Downloads.State = "queued"
					}
				}
				p.ErrorCode = msg.ErrUnknown
				p.ErrorParams = msg.Params{"detail": "音源下载未完成；已完成音源保留，可查看日志并重试剩余项目。"}
			})
			return err
		}
		if e := w.changeResourceDownload(id, func(p *schema.AssistantProposal) { p.Downloads.Items[index].State = "completed" }); e != nil {
			return e
		}
	}
	if e := w.changeResourceDownload(id, func(p *schema.AssistantProposal) {
		if p.Downloads.State == "cancelled" {
			return
		}
		p.Downloads.State = "completed"
		p.ErrorCode = ""
		p.ErrorParams = nil
	}); e != nil {
		return e
	}
	return nil
}

// ControlResourceDownload 只控制已确认的原清单，重试不能扩大到新的目录或地址。
func (w *Workbench) ControlResourceDownload(id, action string) error {
	w.resourceEdit.Lock()
	defer w.resourceEdit.Unlock()
	p := w.findProposal(id)
	if p == nil || p.Downloads == nil || p.State != "confirmed" {
		return fmt.Errorf("该下载清单尚未确认")
	}
	next := cloneDownloadProposal(*p)
	switch action {
	case "resume":
		if next.Downloads.State != "failed" && next.Downloads.State != "paused" {
			return fmt.Errorf("当前任务不能重试")
		}
		if e := w.validateResourceApproval(next); e != nil {
			return e
		}
		next.Downloads.State = "queued"
		next.ErrorCode = ""
		next.ErrorParams = nil
		for i := range next.Downloads.Items {
			if next.Downloads.Items[i].State != "completed" {
				next.Downloads.Items[i].State = "queued"
			}
		}
	case "cancel":
		if next.Downloads.State == "completed" || next.Downloads.State == "cancelled" {
			return nil
		}
		next.Downloads.State = "cancelled"
	default:
		return fmt.Errorf("无效下载操作")
	}
	if e := w.storeProposal(next); e != nil {
		return e
	}
	if action == "resume" {
		w.startResourceDownloadWorker()
	} else {
		w.mu.Lock()
		activity := w.Store.Read().Activity
		var cancel context.CancelFunc
		if activity != nil && activity.RequestID == id {
			cancel = w.cancel
		}
		w.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
	return nil
}

func (w *Workbench) notifyResourceDownloads(id string) {
	w.mu.Lock()
	closed := w.closed
	w.mu.Unlock()
	if closed {
		return
	}
	p := w.findProposal(id)
	if p == nil || p.Downloads == nil || p.Downloads.State != "completed" || p.Downloads.Notified {
		return
	}
	if e := w.changeResourceDownload(id, func(p *schema.AssistantProposal) { p.Downloads.Notified = true }); e != nil {
		return
	}
	text := fmt.Sprintf("音源下载任务 %s 已完成。请用resource_downloads和search_resources核实实际库存后报告结果。", id)
	if p.Downloads.ProjectID != "" {
		text += "请重新读取原作品并预检；只有原任务要求继续备料/拼合且方案已审核时才继续，不要改变未获同意的方案。"
	} else {
		text += "本次只确认了下载，请报告结果，不要自动新建或生成作品。"
	}
	_ = w.sendAssistantToSession(p.Downloads.SessionID, text, AssistantContext{WorkID: p.Downloads.ProjectID, Locale: w.Store.Read().Preferences.UiLocale})
}
