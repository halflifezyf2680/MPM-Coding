package tools

import (
	"testing"
)

// ========== mergePhases 测试 ==========

func TestMergePhases_AllPassed(t *testing.T) {
	old := []Phase{
		{ID: "p1", Name: "阶段1", Status: PhasePassed, Summary: "done"},
		{ID: "p2", Name: "阶段2", Status: PhasePassed, Summary: "done"},
		{ID: "p3", Name: "阶段3", Status: PhasePending},
	}
	newPhases := []Phase{
		{ID: "p3", Name: "阶段3-新版", Status: PhasePending},
		{ID: "p4", Name: "新增阶段", Status: PhasePending},
	}

	result := mergePhases(old, newPhases)

	if len(result) != 4 {
		t.Fatalf("期望 4 个阶段，实际 %d", len(result))
	}

	// 前 2 个是已完成的旧阶段，应原样保留
	if result[0].Summary != "done" || result[1].Summary != "done" {
		t.Error("已完成阶段应保留 summary")
	}
	if result[0].ID != "p1" || result[1].ID != "p2" {
		t.Error("已完成阶段 ID 应保留")
	}

	// 后 2 个是新阶段
	if result[2].Name != "阶段3-新版" {
		t.Errorf("第 3 阶段名称应为 '阶段3-新版'，实际 '%s'", result[2].Name)
	}
	if result[3].Name != "新增阶段" {
		t.Errorf("第 4 阶段名称应为 '新增阶段'，实际 '%s'", result[3].Name)
	}
}

func TestMergePhases_NonePassed(t *testing.T) {
	old := []Phase{
		{ID: "p1", Name: "阶段1", Status: PhasePending},
		{ID: "p2", Name: "阶段2", Status: PhaseActive},
	}
	newPhases := []Phase{
		{ID: "a", Name: "全新A", Status: PhasePending},
		{ID: "b", Name: "全新B", Status: PhasePending},
	}

	result := mergePhases(old, newPhases)

	// 没有已完成阶段，应完全替换
	if len(result) != 2 {
		t.Fatalf("期望 2 个阶段，实际 %d", len(result))
	}
	if result[0].Name != "全新A" || result[1].Name != "全新B" {
		t.Error("应完全使用新阶段")
	}
}

func TestMergePhases_PartiallyPassed(t *testing.T) {
	old := []Phase{
		{ID: "p1", Name: "阶段1", Status: PhasePassed},
		{ID: "p2", Name: "阶段2", Status: PhaseActive}, // 中间卡住
		{ID: "p3", Name: "阶段3", Status: PhasePending},
	}
	newPhases := []Phase{
		{ID: "p2", Name: "阶段2-修改版", Status: PhasePending},
	}

	result := mergePhases(old, newPhases)

	// 只保留 p1（已通过），后面全是新的
	if len(result) != 2 {
		t.Fatalf("期望 2 个阶段，实际 %d: %+v", len(result), result)
	}
	if result[0].ID != "p1" {
		t.Error("第 1 阶段应保留 p1")
	}
	if result[1].Name != "阶段2-修改版" {
		t.Errorf("第 2 阶段应为 '阶段2-修改版'，实际 '%s'", result[1].Name)
	}
}

func TestMergePhases_EmptyOld(t *testing.T) {
	newPhases := []Phase{
		{ID: "a", Name: "A", Status: PhasePending},
	}
	result := mergePhases(nil, newPhases)
	if len(result) != 1 {
		t.Fatalf("期望 1 个阶段，实际 %d", len(result))
	}
}

// ========== 协议状态机核心流转测试 ==========

func TestTaskChainV3_ExecuteLifecycle(t *testing.T) {
	tc := &TaskChainV3{
		TaskID: "test-1",
		Phases: []Phase{
			{ID: "p1", Name: "步骤1", Type: PhaseExecute, Status: PhasePending},
			{ID: "p2", Name: "步骤2", Type: PhaseExecute, Status: PhasePending},
		},
	}

	// 开始 p1
	if err := tc.StartPhase("p1"); err != nil {
		t.Fatal(err)
	}
	if tc.CurrentPhase != "p1" {
		t.Error("当前阶段应为 p1")
	}
	if tc.Phases[0].Status != PhaseActive {
		t.Error("p1 应为 active")
	}

	// 完成 p1
	next, err := tc.CompleteExecute("p1", "完成步骤1")
	if err != nil {
		t.Fatal(err)
	}
	if next != "p2" {
		t.Errorf("下一个阶段应为 p2，实际 %s", next)
	}
	if tc.Phases[0].Status != PhasePassed {
		t.Error("p1 应为 passed")
	}

	// 开始并完成 p2
	_ = tc.StartPhase("p2")
	_, err = tc.CompleteExecute("p2", "完成步骤2")
	if err != nil {
		t.Fatal(err)
	}
	if !tc.IsFinished() {
		t.Error("所有阶段应已完成")
	}
}

func TestTaskChainV3_GatePassFail(t *testing.T) {
	tc := &TaskChainV3{
		TaskID: "test-gate",
		Phases: []Phase{
			{ID: "impl", Name: "实现", Type: PhaseExecute, Status: PhasePending},
			{ID: "gate", Name: "验收", Type: PhaseGate, Status: PhasePending, OnPass: "done", OnFail: "impl", MaxRetries: 3},
			{ID: "done", Name: "完成", Type: PhaseExecute, Status: PhasePending},
		},
	}

	// 完成 impl
	_ = tc.StartPhase("impl")
	_, _ = tc.CompleteExecute("impl", "ok")

	// gate 失败 → 回退到 impl
	_ = tc.StartPhase("gate")
	nextID, retryInfo, err := tc.CompleteGate("gate", "fail", "测试没过")
	if err != nil {
		t.Fatal(err)
	}
	if nextID != "impl" {
		t.Errorf("失败应路由到 impl，实际 %s", nextID)
	}
	if retryInfo == "" {
		t.Error("应有重试信息")
	}
	if tc.Phases[1].Status != PhasePending {
		t.Error("gate 应重置为 pending")
	}

	// 重新 impl
	_ = tc.StartPhase("impl")
	_, _ = tc.CompleteExecute("impl", "ok again")

	// gate 通过 → 路由到 done
	_ = tc.StartPhase("gate")
	nextID, _, err = tc.CompleteGate("gate", "pass", "测试通过")
	if err != nil {
		t.Fatal(err)
	}
	if nextID != "done" {
		t.Errorf("通过应路由到 done，实际 %s", nextID)
	}
}

func TestTaskChainV3_GateMaxRetries(t *testing.T) {
	tc := &TaskChainV3{
		TaskID: "test-retry",
		Phases: []Phase{
			{ID: "gate", Name: "门控", Type: PhaseGate, Status: PhaseActive, MaxRetries: 2},
		},
	}

	// 第 1 次失败
	_, _, _ = tc.CompleteGate("gate", "fail", "fail 1")
	// 第 2 次失败（达到上限）
	_ = tc.StartPhase("gate")
	_, _, err := tc.CompleteGate("gate", "fail", "fail 2")
	if err == nil {
		t.Error("达到最大重试次数应报错")
	}
	if tc.Status != "failed" {
		t.Error("任务应标记为 failed")
	}
}

func TestTaskChainV3_LoopPhase(t *testing.T) {
	tc := &TaskChainV3{
		TaskID: "test-loop",
		Phases: []Phase{
			{ID: "loop1", Name: "并行处理", Type: PhaseLoop, Status: PhaseActive},
			{ID: "final", Name: "收尾", Type: PhaseExecute, Status: PhasePending},
		},
	}

	// spawn 子任务
	subs := []SubTask{
		{ID: "s1", Name: "子任务1", Status: SubTaskPending},
		{ID: "s2", Name: "子任务2", Status: SubTaskPending},
	}
	if err := tc.SpawnSubTasks("loop1", subs); err != nil {
		t.Fatal(err)
	}

	// 开始并完成 s1
	_ = tc.StartSubTask("loop1", "s1")
	allDone, err := tc.CompleteSubTask("loop1", "s1", "pass", "s1 done")
	if err != nil {
		t.Fatal(err)
	}
	if allDone {
		t.Error("s2 还没完成，不应 allDone")
	}

	// 开始并完成 s2
	_ = tc.StartSubTask("loop1", "s2")
	allDone, err = tc.CompleteSubTask("loop1", "s2", "pass", "s2 done")
	if err != nil {
		t.Fatal(err)
	}
	if !allDone {
		t.Error("所有子任务完成，应 allDone")
	}
	if tc.Phases[0].Status != PhasePassed {
		t.Error("loop 阶段应自动标记为 passed")
	}
}

// ========== parsePhasesFromArgs 测试 ==========

func TestParsePhasesFromArgs(t *testing.T) {
	input := []map[string]interface{}{
		{
			"id":      "step1",
			"name":    "步骤一",
			"type":    "gate",
			"input":   "执行代码审计",
			"verify":  "无高危漏洞",
			"on_pass": "step2",
			"on_fail": "step1",
		},
		{
			"id":   "step2",
			"type": "execute",
		},
	}

	phases, err := parsePhasesFromArgs(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(phases) != 2 {
		t.Fatalf("期望 2 个阶段，实际 %d", len(phases))
	}

	p1 := phases[0]
	if p1.Type != PhaseGate {
		t.Error("应为 gate 类型")
	}
	if p1.OnPass != "step2" || p1.OnFail != "step1" {
		t.Error("on_pass/on_fail 应正确解析")
	}
	if p1.Name != "步骤一" {
		t.Error("name 应正确解析")
	}

	p2 := phases[1]
	if p2.Name != "step2" {
		t.Errorf("缺少 name 时应回退到 id，实际 '%s'", p2.Name)
	}
}

func TestParsePhasesFromArgs_MissingID(t *testing.T) {
	input := []map[string]interface{}{
		{"name": "没有 ID"},
	}
	_, err := parsePhasesFromArgs(input)
	if err == nil {
		t.Error("缺少 id 应报错")
	}
}

func TestParseSubTasksFromArgs(t *testing.T) {
	input := []map[string]interface{}{
		{"name": "任务A", "id": "sa"},
		{"name": "任务B"}, // 缺少 id，应自动生成
	}

	subs, err := parseSubTasksFromArgs(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 2 {
		t.Fatalf("期望 2 个子任务，实际 %d", len(subs))
	}
	if subs[0].ID != "sa" {
		t.Error("应保留显式 id")
	}
	if subs[1].ID != "sub_002" {
		t.Errorf("自动生成的 id 应为 'sub_002'，实际 '%s'", subs[1].ID)
	}
}
