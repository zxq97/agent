// Package rentalrules answers only from a versioned, controlled rule catalog.
// It exposes verification requirements instead of inventing supplier terms.
package rentalrules

import (
	"context"
	"strings"

	"github.com/pkg/errors"
	"github.com/zxq97/agent/internal/progress"
)

type Handler struct {
	catalog Catalog
}

func New(catalog Catalog) *Handler {
	if catalog == nil {
		catalog = NewDefaultCatalog()
	}
	return &Handler{catalog: catalog}
}

func (h *Handler) Handle(ctx context.Context, input *Input) (*Result, error) {
	if input == nil || strings.TrimSpace(input.EvidenceText) == "" {
		return nil, errors.New("rental rules: evidence text is required")
	}
	progress.Emit(ctx, "rental_rules", "正在查询租车规则")
	rules := h.catalog.Search(input.EvidenceText)
	if len(rules) == 0 {
		return &Result{
			Status:         StatusInsufficient,
			CatalogVersion: h.catalog.Version(),
			Message:        "当前规则目录没有覆盖这个问题，不能据此编造供应商或订单规则；请查看具体报价确认页或咨询门店。",
		}, nil
	}
	return &Result{
		Status:         StatusSuccess,
		CatalogVersion: h.catalog.Version(),
		Rules:          rules,
		Message:        "以下是通用核对指引，不是当前供应商或订单的最终承诺；最终以报价确认页、供应商和门店条款为准。",
	}, nil
}
