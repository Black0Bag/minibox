package docs

import (
	"strings"
	"testing"
)

func TestBuildRoutes(t *testing.T) {
	doc := Build("v1.0.0")
	if doc.Version != "v1.0.0" {
		t.Errorf("版本错误: %s", doc.Version)
	}
	if len(doc.Routes) < 40 {
		t.Errorf("路由数应不少于 40，得到 %d", len(doc.Routes))
	}
}

func TestMarkdownOutput(t *testing.T) {
	md := Build("v1.0.0").Markdown()
	if !strings.Contains(md, "minibox 后端 API 文档") {
		t.Error("Markdown 应含标题")
	}
	if !strings.Contains(md, "/api/v1/知识库") {
		t.Error("Markdown 应含知识库路由")
	}
	if !strings.Contains(md, "| GET |") {
		t.Error("Markdown 应含表格")
	}
}

func TestSortedRoutes(t *testing.T) {
	doc := Build("v1")
	for i := 1; i < len(doc.Routes); i++ {
		prev, cur := doc.Routes[i-1], doc.Routes[i]
		if prev.Path > cur.Path {
			t.Errorf("路由未排序: %s > %s", prev.Path, cur.Path)
		}
	}
}
