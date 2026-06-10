package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
	"go.uber.org/zap"
)

// KnowledgeSearchInput 知识库检索输入
type KnowledgeSearchInput struct {
	Query string `json:"query" jsonschema:"description=检索关键词或问题" jsonschema_description:"检索关键词或问题"`
}

// KnowledgeSearchOutput 知识库检索输出
type KnowledgeSearchOutput struct {
	Results []KnowledgeItem `json:"results"`
}

// KnowledgeItem 知识条目
type KnowledgeItem struct {
	Source   string `json:"source"`
	Content  string `json:"content"`
	Relevance string `json:"relevance"`
}

// Provider 本地知识库 ToolProvider
type Provider struct {
	baseDir  string
	domain   string
	contents []knowledgeContent
}

type knowledgeContent struct {
	source  string
	content string
	keywords []string
}

// NewProvider 创建本地知识库 Provider
func NewProvider(baseDir, domain string) *Provider {
	return &Provider{
		baseDir: baseDir,
		domain:  domain,
	}
}

// Name 返回 ToolProvider 标识
func (p *Provider) Name() string {
	return fmt.Sprintf("knowledge_%s", p.domain)
}

// LoadTools 加载知识库检索 Tool
func (p *Provider) LoadTools(ctx context.Context) ([]tool.BaseTool, error) {
	// 加载知识库内容到内存
	if err := p.load(); err != nil {
		return nil, fmt.Errorf("加载 %s 知识库失败: %w", p.domain, err)
	}

	toolName := fmt.Sprintf("search_%s_knowledge", p.domain)
	toolDesc := fmt.Sprintf("从%s知识库中检索相关规则、车型信息或常见问题解答", domainName(p.domain))

	t, err := utils.InferTool[KnowledgeSearchInput, KnowledgeSearchOutput](
		toolName,
		toolDesc,
		p.search,
	)
	if err != nil {
		return nil, fmt.Errorf("创建知识库 Tool 失败: %w", err)
	}

	return []tool.BaseTool{t}, nil
}

// load 从文件加载知识库内容到内存
func (p *Provider) load() error {
	dir := filepath.Join(p.baseDir, p.domain)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("读取目录 %s 失败: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		content := string(data)
		ext := strings.ToLower(filepath.Ext(entry.Name()))

		switch ext {
		case ".json":
			// JSON 文件：逐条解析
			p.loadJSON(entry.Name(), content)
		case ".md", ".markdown":
			// Markdown 文件：按段落拆分
			p.loadMarkdown(entry.Name(), content)
		}
	}

	return nil
}

// loadJSON 解析 JSON 知识库文件
func (p *Provider) loadJSON(filename, content string) {
	// 尝试解析为数组
	var items []map[string]any
	if err := json.Unmarshal([]byte(content), &items); err != nil {
		// 解析失败，作为整段存储
		p.contents = append(p.contents, knowledgeContent{
			source:   filename,
			content:  content,
			keywords: extractKeywords(content),
		})
		return
	}

	for _, item := range items {
		// 将 JSON 对象重新序列化为可读文本
		text := formatJSONItem(item)
		p.contents = append(p.contents, knowledgeContent{
			source:   filename,
			content:  text,
			keywords: extractKeywords(text),
		})
	}
}

// loadMarkdown 解析 Markdown 知识库文件
func (p *Provider) loadMarkdown(filename, content string) {
	// 按二级标题拆分段落
	sections := strings.Split(content, "\n## ")

	for i, section := range sections {
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}

		// 第一段可能没有 ## 前缀
		if i == 0 && !strings.HasPrefix(section, "# ") {
			section = "# " + section
		} else if i > 0 {
			section = "## " + section
		}

		p.contents = append(p.contents, knowledgeContent{
			source:   filename,
			content:  section,
			keywords: extractKeywords(section),
		})
	}
}

// search 执行知识库检索
func (p *Provider) search(ctx context.Context, input KnowledgeSearchInput) (KnowledgeSearchOutput, error) {
	zap.L().Info("[Knowledge] 检索请求",
		zap.String("domain", p.domain),
		zap.String("query", input.Query),
	)

	query := strings.ToLower(input.Query)
	queryWords := strings.Fields(query)

	var results []KnowledgeItem

	for _, c := range p.contents {
		score := 0
		contentLower := strings.ToLower(c.content)

		for _, word := range queryWords {
			if strings.Contains(contentLower, word) {
				score++
			}
			for _, kw := range c.keywords {
				if strings.Contains(strings.ToLower(kw), word) {
					score += 2
				}
			}
		}

		if score > 0 {
			relevance := "低"
			if score >= 4 {
				relevance = "高"
			} else if score >= 2 {
				relevance = "中"
			}

			results = append(results, KnowledgeItem{
				Source:    c.source,
				Content:   c.content,
				Relevance: relevance,
			})
		}
	}

	// 按相关度排序（高 → 中 → 低）
	sortByRelevance(results)

	// 最多返回5条
	if len(results) > 5 {
		results = results[:5]
	}

	if len(results) == 0 {
		zap.L().Info("[Knowledge] 无匹配结果",
			zap.String("domain", p.domain),
			zap.String("query", input.Query),
		)
		results = []KnowledgeItem{{
			Source:   "系统",
			Content:  fmt.Sprintf("未在%s知识库中找到与\"%s\"相关的内容", domainName(p.domain), input.Query),
			Relevance: "无",
		}}
	} else {
		zap.L().Info("[Knowledge] 检索结果",
			zap.String("domain", p.domain),
			zap.Int("result_count", len(results)),
			zap.String("top_relevance", results[0].Relevance),
			zap.String("top_source", results[0].Source),
		)
	}

	return KnowledgeSearchOutput{Results: results}, nil
}

// extractKeywords 从文本中提取关键词
func extractKeywords(text string) []string {
	var keywords []string

	// 提取中文字符序列和英文单词
	var buf strings.Builder
	for _, r := range text {
		if r >= 0x4e00 && r <= 0x9fff {
			buf.WriteRune(r)
		} else if buf.Len() > 0 {
			word := buf.String()
			if len(word) >= 2 {
				keywords = append(keywords, word)
			}
			buf.Reset()
		}
	}
	if buf.Len() > 0 {
		word := buf.String()
		if len(word) >= 2 {
			keywords = append(keywords, word)
		}
	}

	return keywords
}

// formatJSONItem 将 JSON 对象格式化为可读文本
func formatJSONItem(item map[string]any) string {
	var sb strings.Builder
	for k, v := range item {
		switch val := v.(type) {
		case string:
			sb.WriteString(fmt.Sprintf("%s: %s\n", k, val))
		case []any:
			sb.WriteString(fmt.Sprintf("%s: %s\n", k, formatSlice(val)))
		default:
			sb.WriteString(fmt.Sprintf("%s: %v\n", k, val))
		}
	}
	return sb.String()
}

// formatSlice 格式化数组
func formatSlice(items []any) string {
	strs := make([]string, 0, len(items))
	for _, item := range items {
		strs = append(strs, fmt.Sprintf("%v", item))
	}
	return strings.Join(strs, "、")
}

// sortByRelevance 按相关度排序
func sortByRelevance(items []KnowledgeItem) {
	order := map[string]int{"高": 3, "中": 2, "低": 1, "无": 0}
	for i := 0; i < len(items); i++ {
		for j := i + 1; j < len(items); j++ {
			if order[items[j].Relevance] > order[items[i].Relevance] {
				items[i], items[j] = items[j], items[i]
			}
		}
	}
}

// domainName 返回域的中文名
func domainName(domain string) string {
	names := map[string]string{
		"vehicle":     "车辆",
		"insurance":   "保险",
		"billing":     "费用",
		"fulfillment": "履约",
	}
	if name, ok := names[domain]; ok {
		return name
	}
	return domain
}

// 确保 Provider 实现了 ToolProvider 接口
var _ interface {
	Name() string
	LoadTools(ctx context.Context) ([]tool.BaseTool, error)
} = (*Provider)(nil)
