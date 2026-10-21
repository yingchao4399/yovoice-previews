package workbench

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"yovoice/internal/msg"
	"yovoice/internal/schema"
)

func TestAuditionRetryWaitsForNextItemAndRetainsFeedback(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	d, ref := recoveryDraft(t, w)
	m := recoveryCandidate(t, w, d, ref, "needs_review")
	gate := make(chan struct{})
	var order []string
	done, err := w.start("generate", msg.ActivityGenerate, nil, nil, func(ctx context.Context) error {
		<-gate
		order = append(order, "next")
		err := w.drainArrangementRetries(ctx, d, func(task arrangementGenerationTask, in ArrangementAIPrepareInput) error {
			order = append(order, "retry")
			if in.RetryFeedback != "柔和一点" || in.RepairRound != 1 || task.job.part.Instrument != m.Instrument || task.contract.CoreSeconds != m.Generation.CoreSeconds {
				t.Errorf("重试没有保留反馈或原合同：%+v %+v", task.contract, in)
			}
			return nil
		})
		order = append(order, "later")
		return err
	}, "", d.ID)
	must(t, err)
	round, err := w.RetryArrangementMaterialWithFeedback(m.ID, d, "柔和一点")
	must(t, err)
	if round != 1 {
		t.Fatalf("首轮编号：%d", round)
	}
	round, err = w.RetryArrangementMaterialWithFeedback(m.ID, d, "重复点击")
	must(t, err)
	if round != 1 {
		t.Fatal("重复提交增加了轮次")
	}
	saved := w.Store.Read().ArrangementMaterials[0]
	if saved.RetryRequest.Status != "queued" || saved.GenerationResult.State != "needs_review" {
		t.Fatal("排队时丢失原候选")
	}
	if w.ReviewArrangementMaterial(m.ID, true) == nil {
		t.Fatal("已请求重做的旧候选不应被采用")
	}
	if w.DeleteArrangementMaterial(m.ID) == nil {
		t.Fatal("排队期间删除旧候选会造成后台恢复已删除记录")
	}
	if _, err := w.FitArrangementMaterial(ArrangementMaterialFitInput{ID: m.ID, Seconds: m.Generation.CoreSeconds}); err == nil {
		t.Fatal("排队期间不能修改旧候选的区段合同")
	}
	close(gate)
	<-done
	if !reflect.DeepEqual(order, []string{"next", "retry", "later"}) {
		t.Fatalf("执行顺序：%v", order)
	}
	saved = w.Store.Read().ArrangementMaterials[0]
	if saved.RetryRequest.Status != "completed" || saved.GenerationResult.State != "rejected" {
		t.Fatalf("未完成重试交接：%+v", saved)
	}
}

func TestAuditionRetryRejectsChangedScoreAndSurvivesRestart(t *testing.T) {
	root := t.TempDir()
	w, err := New(root)
	must(t, err)
	d, ref := recoveryDraft(t, w)
	m := recoveryCandidate(t, w, d, ref, "needs_review")
	m.RetryRequest = &schema.ArrangementMaterialRetry{Status: "queued", Feedback: "自然收尾", Round: 1}
	must(t, w.persistGenerationMaterial(m))
	w.Close()
	w, err = New(root)
	must(t, err)
	defer w.Close()
	if w.Store.Read().ArrangementMaterials[0].RetryRequest.Feedback != "自然收尾" {
		t.Fatal("重启丢失用户试听意见")
	}
	d.Score.Tracks[1].Notes[0].Pitch++
	must(t, w.SaveDraft(d))
	err = w.drainArrangementRetries(context.Background(), d, func(arrangementGenerationTask, ArrangementAIPrepareInput) error {
		t.Fatal("曲谱已变更仍执行旧合同")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "变更") {
		t.Fatalf("错误未定位旧修订：%v", err)
	}
	if saved := w.Store.Read().ArrangementMaterials[0]; saved.RetryRequest.Status != "failed" || saved.GenerationResult.State != "needs_review" {
		t.Fatal("失败应保留原候选")
	}
}

func TestQueuedAuditionRetryCanBeDeletedWhileIdle(t *testing.T) {
	w, err := New(t.TempDir())
	must(t, err)
	defer w.Close()
	d, ref := recoveryDraft(t, w)
	m := recoveryCandidate(t, w, d, ref, "needs_review")
	m.RetryRequest = &schema.ArrangementMaterialRetry{Status: "queued", Round: 1}
	must(t, w.persistGenerationMaterial(m))
	must(t, w.DeleteArrangementMaterial(m.ID))
	if len(w.Store.Read().ArrangementMaterials) != 0 {
		t.Fatal("空闲时应能放弃未执行的排队候选")
	}
}
