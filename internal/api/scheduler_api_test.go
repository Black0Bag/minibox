package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

const (
	schedPath = "/api/v1/%E8%B0%83%E5%BA%A6"   // 调度
	beatPath  = "/api/v1/%E5%BF%83%E8%B7%B3"  // 心跳
	skillPath = "/api/v1/%E6%8A%80%E8%83%BD"  // 技能
)

func TestScheduleAPI(t *testing.T) {
	s := testServer(t, nil)
	// 创建
	rec := doReq(t, s, http.MethodPost, schedPath+"/", `{"type":"alarm","time":"12:00","desc":"午休","payload":"去吃饭"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建应为 201，得到 %d: %s", rec.Code, rec.Body.String())
	}
	var created struct{ ID string `json:"id"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" {
		t.Fatal("应生成 id")
	}
	// 校验错误
	rec = doReq(t, s, http.MethodPost, schedPath+"/", `{"type":"bad","time":"12:00","desc":"x"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法类型应 400，得到 %d", rec.Code)
	}
	// 列表
	rec = doReq(t, s, http.MethodGet, schedPath+"/", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("列表应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "午休") {
		t.Errorf("列表应含午休: %s", rec.Body.String())
	}
	// 获取
	rec = doReq(t, s, http.MethodGet, schedPath+"/"+created.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("获取应为 200，得到 %d", rec.Code)
	}
	// 执行
	rec = doReq(t, s, http.MethodPost, schedPath+"/"+created.ID+"/%E6%89%A7%E8%A1%8C", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("执行应为 200，得到 %d", rec.Code)
	}
	// 取消
	rec = doReq(t, s, http.MethodPost, schedPath+"/"+created.ID+"/%E5%8F%96%E6%B6%88", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("取消应为 200，得到 %d", rec.Code)
	}
	// 删除
	rec = doReq(t, s, http.MethodDelete, schedPath+"/"+created.ID, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("删除应为 204，得到 %d", rec.Code)
	}
}

func TestHeartbeatAPI(t *testing.T) {
	s := testServer(t, nil)
	// 创建（间隔 <300 应 400）
	rec := doReq(t, s, http.MethodPost, beatPath+"/", `{"desc":"检查磁盘","interval":60}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("小间隔应 400，得到 %d: %s", rec.Code, rec.Body.String())
	}
	// 合法创建
	rec = doReq(t, s, http.MethodPost, beatPath+"/", `{"desc":"检查磁盘","interval":300,"prompt":"检查磁盘空间","boundaries":"只读操作"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建应为 201，得到 %d: %s", rec.Code, rec.Body.String())
	}
	var created struct{ ID string `json:"id"` }
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	// 列表
	rec = doReq(t, s, http.MethodGet, beatPath+"/", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("列表应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "检查磁盘") {
		t.Errorf("列表应含检查磁盘: %s", rec.Body.String())
	}
	// 退订
	rec = doReq(t, s, http.MethodPost, beatPath+"/"+created.ID+"/%E9%80%80%E8%AE%A2", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("退订应为 200，得到 %d", rec.Code)
	}
}

func TestSkillAPI(t *testing.T) {
	s := testServer(t, nil)
	// 创建
	rec := doReq(t, s, http.MethodPost, skillPath+"/", `{"name":"编译知识库","desc":"把文本编译成知识库条目","steps":["提炼","入库"],"tags":["编译"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("创建应为 201，得到 %d: %s", rec.Code, rec.Body.String())
	}
	// 校验错误
	rec = doReq(t, s, http.MethodPost, skillPath+"/", `{"name":""}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空名应 400，得到 %d", rec.Code)
	}
	// 匹配
	rec = doReq(t, s, http.MethodGet, skillPath+"/%E5%8C%B9%E9%85%8D?q=%E7%BC%96%E8%AF%91%E8%BF%99%E4%B8%AA%E6%96%87%E6%A1%A3", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("匹配应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"命中":true`) {
		t.Errorf("应命中: %s", rec.Body.String())
	}
	// 沉淀
	rec = doReq(t, s, http.MethodPost, skillPath+"/%E6%B2%89%E6%B7%80", `{"name":"写周报","desc":"整理周报","steps":["收集","生成"]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("沉淀应为 201，得到 %d: %s", rec.Code, rec.Body.String())
	}
	// 列表
	rec = doReq(t, s, http.MethodGet, skillPath+"/", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("列表应为 200，得到 %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "编译知识库") {
		t.Errorf("列表应含编译知识库: %s", rec.Body.String())
	}
}
