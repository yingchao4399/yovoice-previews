package workbench

import (
	"context"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"slices"
	"yovoice/internal/schema"
)

type arrangementToolInput struct {
	ProjectID string `json:"projectId" jsonschema:"当前编曲作品 ID，先用 list_works 获取"`
	AIModelID string `json:"aiModelId,omitempty" jsonschema:"准备AI音频的已安装模型ID，DeepSeek只负责规划和写谱"`
}

func (w *Workbench) registerArrangementTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{Name: "apply_resource_band", Description: "将search_resources返回的乐队组合应用于当前计划；只匹配同乐器、角色、来源和可用奏法，保留锁定声部及已有AI模型选择，不复制旧歌曲。先get_work和查询kind=band，组合缺文件或不能演奏当前谱时报告具体原因，不自动下载。"}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		ProjectID  string `json:"projectId"`
		ResourceID string `json:"resourceId"`
	}) (*mcp.CallToolResult, any, error) {
		d, err := w.assistantWork(in.ProjectID)
		if err != nil {
			return nil, nil, err
		}
		fixed, err := w.ApplyResourceBand(ResourceBandInput{Draft: d, ResourceID: in.ResourceID})
		return nil, map[string]any{"saved": err == nil, "projectId": d.ID, "arrangement": fixed.Arrangement}, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "add_resource_clip", Description: "把库存中实际存在的本地音频（已有AI乐句、已生成歌曲、导入音频或单个采样）复制为工程素材并放到独立可编辑音轨；start/offset/duration以秒为单位，duration=0使用剩余长度。先search_resources确认ID与来源，先get_work读取工程。遵守真实/AI来源限制，原文件保持；整曲是片段素材，不会假装可逐音演奏。重拼合保留该手工轨道。"}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		ProjectID  string  `json:"projectId"`
		ResourceID string  `json:"resourceId"`
		Start      float64 `json:"start"`
		Offset     float64 `json:"offset,omitempty"`
		Duration   float64 `json:"duration,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		d, err := w.assistantWork(in.ProjectID)
		if err != nil {
			return nil, nil, err
		}
		fixed, err := w.AddResourceClip(ctx, ResourceClipInput{Draft: d, ResourceID: in.ResourceID, Start: in.Start, Offset: in.Offset, Duration: in.Duration})
		return nil, map[string]any{"saved": err == nil, "projectId": d.ID, "timeline": fixed.Timeline}, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "search_resources", Description: "查询应用真实库存，支持 kind=instrument/sample/audio/voice/band/model/catalog、query、origin、parentId 和分页；本地健康素材排在前面。sample 是逐个单音采样，audio 是已生成或导入的完整片段，不能当作单音乐器；catalog 待下载只算候选，须用户确认后才能下载。选择声部前先查具体采样奏法、音高与文件健康；不能虚构路径。"}, func(ctx context.Context, req *mcp.CallToolRequest, in ResourceQuery) (*mcp.CallToolResult, any, error) {
		out, err := w.SearchResources(in)
		return nil, out, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "arrangement_inventory", Description: "盘点本地音源和真实文件健康状态，含采样音高、奏法、力度层与插件音色；网页或目录不等于已安装音源。"}, func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
		var out []map[string]any
		for _, s := range w.Store.Read().InstrumentSources {
			item := map[string]any{"id": s.ID, "name": s.Name, "instrument": s.Instrument, "origin": s.Origin, "provenance": s.Provenance, "verification": s.Verification, "reviewRequired": s.Verification == "metadata", "fileIssue": w.sourceFilesIssue(s), "samples": s.Samples}
			if s.Plugin != nil {
				item["plugin"] = map[string]any{"name": s.Plugin.Name, "articulations": s.Plugin.Articulations}
			}
			out = append(out, item)
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{Name: "inspect_arrangement", Description: "只读检查逐句编曲、真实文件、乐器身份、奏法、力度和音域，并返回库中可用候选。下载或改绑之前先调用。候选中的 ensemble 表示合奏采样，不能称为真人独奏。"}, func(ctx context.Context, req *mcp.CallToolRequest, in arrangementToolInput) (*mcp.CallToolResult, any, error) {
		d, e := w.assistantWork(in.ProjectID)
		if e != nil {
			return nil, nil, e
		}
		r, e := w.InspectArrangement(ArrangementInspectInput{Draft: d, AIModelID: in.AIModelID, Stage: "render"})
		return nil, r, e
	})
	mcp.AddTool(server, &mcp.Tool{Name: "bind_arrangement_source", Description: "给一个计划声部绑定已安装音源，不改原音符。锁定声部拒绝修改；非同类乐器需要用户明确允许替代后才可设置 allowSubstitution。"}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		ProjectID         string              `json:"projectId"`
		PartID            string              `json:"partId,omitempty"`
		Ref               *ArrangementPartRef `json:"ref,omitempty"`
		SourceID          string              `json:"sourceId"`
		AllowSubstitution bool                `json:"allowSubstitution,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		d, e := w.assistantWork(in.ProjectID)
		if e != nil {
			return nil, nil, e
		}
		base := arrangementRevision(d)
		fixed, e := w.BindArrangementSource(ArrangementBindInput{Draft: d, PartID: in.PartID, Ref: in.Ref, SourceID: in.SourceID, AllowSubstitution: in.AllowSubstitution})
		if e != nil {
			return nil, nil, e
		}
		if e = w.saveAssistantArrangement(fixed, base); e != nil {
			return nil, nil, e
		}
		w.chatCard("bind_arrangement_source", "已绑定所选音源", "Bound instrument source", "done")
		return nil, map[string]any{"saved": true, "projectId": d.ID}, nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "plan_arrangement", Description: "按用户需求和本地库存规划整曲分句配器并保存未审核方案。source为real/ai/hybrid/auto；先盘点，不能虚构已下载音源。"}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		ProjectID string `json:"projectId"`
		Brief     string `json:"brief"`
		Seconds   int    `json:"seconds"`
		Source    string `json:"source"`
	}) (*mcp.CallToolResult, any, error) {
		d, e := w.assistantWork(in.ProjectID)
		if e != nil {
			return nil, nil, e
		}
		if d.Kind != "score" {
			return nil, nil, fmt.Errorf("请先新建编曲作品")
		}
		base := arrangementRevision(d)
		plan, e := w.PlanArrangement(ctx, ArrangementBrief{ProjectID: d.ID, Brief: in.Brief, Seconds: in.Seconds, Source: in.Source, Locale: w.Store.Read().Preferences.UiLocale})
		if e != nil {
			return nil, nil, e
		}
		plan.Brief = in.Brief
		d.Arrangement = &plan
		d.ArrangementInput = &schema.ArrangementInput{Brief: in.Brief, Seconds: in.Seconds, Source: in.Source}
		e = w.saveAssistantArrangement(d, base)
		return nil, plan, e
	})
	mcp.AddTool(server, &mcp.Tool{Name: "compose_arrangement", Description: "为已经审核的编曲计划逐乐句写可编辑乐谱并保存，按实际已绑定音源校验。未审核方案拒绝执行，请先让用户审阅；不会准备或生成音频。"}, func(ctx context.Context, req *mcp.CallToolRequest, in arrangementToolInput) (*mcp.CallToolResult, any, error) {
		d, e := w.assistantWork(in.ProjectID)
		if e != nil {
			return nil, nil, e
		}
		if d.Arrangement == nil || !d.Arrangement.Approved {
			return nil, nil, fmt.Errorf("请先在智能编曲页面审核方案")
		}
		base := arrangementRevision(d)
		music, e := w.ComposeScore(ctx, ScoreBrief{ProjectID: d.ID, Brief: d.Arrangement.Brief, Seconds: d.Arrangement.Seconds, Arrangement: d.Arrangement, Locale: w.Store.Read().Preferences.UiLocale})
		if e != nil {
			return nil, nil, e
		}
		d.Score = &music.Score
		e = w.saveAssistantArrangement(d, base)
		return nil, map[string]any{"saved": e == nil, "projectId": d.ID}, e
	})
	mcp.AddTool(server, &mcp.Tool{Name: "prepare_arrangement", Description: "对已审核编曲执行持久备料任务。mode=real/ai/both；返回job后用arrangement_jobs查询。未完成不得声称生成成功；只在用户要求备料后调用。"}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		ProjectID string   `json:"projectId"`
		Mode      string   `json:"mode"`
		AIModelID string   `json:"aiModelId,omitempty"`
		PartIDs   []string `json:"partIds,omitempty"`
		ResumeID  string   `json:"resumeId,omitempty"`
	}) (*mcp.CallToolResult, any, error) {
		d, e := w.assistantWork(in.ProjectID)
		if e != nil {
			return nil, nil, e
		}
		needsReal := in.Mode != "ai"
		if in.ResumeID != "" {
			for _, previous := range w.ArrangementJobs(in.ProjectID) {
				if previous.ID == in.ResumeID && previous.Mode != "ai" {
					needsReal = true
				}
			}
		}
		if needsReal {
			if e = w.requireLocalPreparationSources(d, in.PartIDs); e != nil {
				return nil, nil, e
			}
		}
		job, e := w.PrepareArrangement(ArrangementPrepareInput{Draft: d, Mode: in.Mode, AIModelID: in.AIModelID, PartIDs: in.PartIDs, ResumeID: in.ResumeID})
		return nil, job, e
	})
	mcp.AddTool(server, &mcp.Tool{Name: "arrangement_jobs", Description: "读取当前作品持久备料批次和每个声部的真实进度、失败原因；已完成素材不会因后续失败丢失。"}, func(ctx context.Context, req *mcp.CallToolRequest, in arrangementToolInput) (*mcp.CallToolResult, any, error) {
		return nil, w.ArrangementJobs(in.ProjectID), nil
	})
	mcp.AddTool(server, &mcp.Tool{Name: "retry_arrangement_material", Description: "只重试当前作品中指定的失败 AI 候选，最多三轮自主修复，并保留此前成果。调用前先 inspect_arrangement、读取 arrangement_jobs 和候选检查原因；工具会拒绝时长、内容或能力合同不匹配的原样重试。第三轮仍失败时必须在第四轮通过 ask_user 问答卡让用户选择局部/整体重新规划或其他处理；不得改变用户的音乐意图、乐器和来源限制。"}, func(ctx context.Context, req *mcp.CallToolRequest, in struct {
		ProjectID  string `json:"projectId"`
		MaterialID string `json:"materialId"`
	}) (*mcp.CallToolResult, any, error) {
		d, err := w.assistantWork(in.ProjectID)
		if err != nil {
			return nil, nil, err
		}
		if in.MaterialID == "" {
			return nil, nil, fmt.Errorf("请提供需要重试的失败候选 materialId")
		}
		round, err := w.RetryArrangementMaterialWithRound(in.MaterialID, d)
		if err != nil && round >= 3 {
			q, questionErr := w.AskAssistantQuestion(AssistantQuestionInput{
				Prompt:  "这个声部经过三轮局部修复仍未通过。下一步会改变任务范围，请选择处理方式。",
				Options: []string{"只重新规划并生成这个声部，保留其他已完成部分", "重新规划整首编曲", "先查看失败原因和可选方案，暂不重规划"},
			})
			if questionErr != nil {
				return nil, nil, questionErr
			}
			return nil, map[string]any{"state": "awaiting_user", "repairRound": round, "question": q}, nil
		}
		return nil, map[string]any{"started": err == nil, "repairRound": round, "projectId": d.ID, "materialId": in.MaterialID}, err
	})
	mcp.AddTool(server, &mcp.Tool{Name: "render_arrangement", Description: "预检后把真实采样和已准备AI乐句拼合到可编辑多轨时间线；缺音源拒绝执行，不退回SF2。调用返回仅代表已启动，用get_generation_status确认结果。"}, func(ctx context.Context, req *mcp.CallToolRequest, in arrangementToolInput) (*mcp.CallToolResult, any, error) {
		d, e := w.assistantWork(in.ProjectID)
		if e != nil {
			return nil, nil, e
		}
		e = w.RenderArrangement(ArrangementRenderInput{Draft: d, AIModelID: in.AIModelID})
		return nil, map[string]any{"started": e == nil, "projectId": d.ID}, e
	})
}

// 助手不能借“备料”绕过下载卡。界面用户明确点击下载的路径仍使用原备料服务。
func (w *Workbench) requireLocalPreparationSources(d schema.Draft, partIDs []string) error {
	inspection, e := w.InspectArrangement(ArrangementInspectInput{Draft: d, Stage: "prepare"})
	if e != nil {
		return e
	}
	for _, part := range inspection.Parts {
		if len(partIDs) > 0 && part.PartID != "" && !slices.Contains(partIDs, part.PartID) {
			continue
		}
		ref := part.Ref
		planned := d.Arrangement.Sections[ref.SectionIndex].Phrases[ref.PhraseIndex].Parts[ref.PartIndex]
		if planned.Source != "real" {
			continue
		}
		if !part.Ready || part.SourceID == "" {
			return fmt.Errorf("%s · %s 尚无适用的健康本地采样。先用search_resources诊断；需要CC0下载时用propose_resource_download显示精确清单，用户确认并完成后再备料。", part.Phrase, part.Instrument)
		}
	}
	return nil
}

func (w *Workbench) saveAssistantArrangement(d schema.Draft, revision string) error {
	w.edit.Lock()
	defer w.edit.Unlock()
	current, e := w.assistantWork(d.ID)
	if e != nil {
		return e
	}
	if arrangementRevision(current) != revision {
		return fmt.Errorf("用户在处理期间修改了作品，请重新读取再操作")
	}
	if e = w.saveDraft(d); e == nil {
		w.chatUpdate(d.ID)
	}
	return e
}
