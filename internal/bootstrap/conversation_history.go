package bootstrap

import (
	"context"
	"sort"
	"strconv"

	"github.com/EurekaMXZ/assistant/internal/domain"
	"github.com/EurekaMXZ/assistant/internal/postgres"
	"github.com/EurekaMXZ/assistant/internal/server"
)

func listConversationTurnSummariesPage(
	ctx context.Context,
	repository *postgres.TurnRepository,
	conversationID string,
	limit int,
	beforeSeq int64,
	afterSeq int64,
	query string,
) (*server.ConversationTurnPage, error) {
	items, err := repository.ListConversationTurnSummaries(ctx, conversationID, limit+1, beforeSeq, afterSeq, query)
	if err != nil {
		return nil, err
	}
	page := &server.ConversationTurnPage{Items: items}
	if beforeSeq > 0 || afterSeq == 0 {
		if len(page.Items) > limit {
			page.HasMoreBefore = true
			page.Items = page.Items[len(page.Items)-limit:]
		}
		if page.HasMoreBefore && len(page.Items) > 0 {
			page.NextBefore = strconv.FormatInt(page.Items[0].Seq, 10)
		}
	} else if len(page.Items) > limit {
		page.HasMoreAfter = true
		page.Items = page.Items[:limit]
		if len(page.Items) > 0 {
			page.NextAfter = strconv.FormatInt(page.Items[len(page.Items)-1].Seq, 10)
		}
	}
	return page, nil
}

func getConversationTurnContextPage(
	ctx context.Context,
	turns *postgres.TurnRepository,
	events *postgres.ConversationEventRepository,
	conversationID string,
	centerSeq int64,
	beforeLimit int,
	afterLimit int,
	beforeSeq int64,
	afterSeq int64,
) (*server.ConversationTurnContextPage, error) {
	if beforeLimit <= 0 {
		beforeLimit = 3
	}
	if afterLimit <= 0 {
		afterLimit = 3
	}

	var (
		selected      []domain.ConversationTurnSummary
		hasMoreBefore bool
		hasMoreAfter  bool
		nextBefore    string
		nextAfter     string
	)

	if centerSeq > 0 {
		window, err := turns.ListConversationTurnWindow(ctx, conversationID, centerSeq, beforeLimit, afterLimit)
		if err != nil {
			return nil, err
		}
		centerIndex := -1
		for index, item := range window {
			if item.Seq == centerSeq {
				centerIndex = index
				break
			}
		}
		if centerIndex == -1 {
			return nil, domain.ErrNotFound
		}

		beforeItems := append([]domain.ConversationTurnSummary(nil), window[:centerIndex]...)
		afterItems := append([]domain.ConversationTurnSummary(nil), window[centerIndex+1:]...)
		if len(beforeItems) > beforeLimit {
			hasMoreBefore = true
			beforeItems = beforeItems[len(beforeItems)-beforeLimit:]
		}
		if len(afterItems) > afterLimit {
			hasMoreAfter = true
			afterItems = afterItems[:afterLimit]
		}
		selected = append(selected, beforeItems...)
		selected = append(selected, window[centerIndex])
		selected = append(selected, afterItems...)
	} else {
		if beforeSeq == 0 && afterSeq == 0 {
			limit := beforeLimit + afterLimit + 1
			items, err := turns.ListConversationTurnSummaries(ctx, conversationID, limit+1, 0, 0, "")
			if err != nil {
				return nil, err
			}
			if len(items) > limit {
				hasMoreBefore = true
				items = items[len(items)-limit:]
			}
			selected = items
		} else {
			if beforeSeq > 0 {
				items, err := turns.ListConversationTurnSummaries(ctx, conversationID, beforeLimit+1, beforeSeq, 0, "")
				if err != nil {
					return nil, err
				}
				if len(items) > beforeLimit {
					hasMoreBefore = true
					items = items[:beforeLimit]
				}
				selected = append(selected, items...)
			}
			if afterSeq > 0 {
				items, err := turns.ListConversationTurnSummaries(ctx, conversationID, afterLimit+1, 0, afterSeq, "")
				if err != nil {
					return nil, err
				}
				if len(items) > afterLimit {
					hasMoreAfter = true
					items = items[:afterLimit]
				}
				selected = append(selected, items...)
			}
		}
	}

	sort.Slice(selected, func(i, j int) bool { return selected[i].Seq < selected[j].Seq })
	if hasMoreBefore && len(selected) > 0 {
		if beforeSeq > 0 {
			nextBefore = strconv.FormatInt(selected[0].Seq, 10)
		} else {
			for _, item := range selected {
				if centerSeq == 0 || item.Seq < centerSeq {
					nextBefore = strconv.FormatInt(item.Seq, 10)
					break
				}
			}
		}
	}
	if hasMoreAfter && len(selected) > 0 {
		if afterSeq > 0 {
			nextAfter = strconv.FormatInt(selected[len(selected)-1].Seq, 10)
		} else {
			for index := len(selected) - 1; index >= 0; index-- {
				if centerSeq == 0 || selected[index].Seq > centerSeq {
					nextAfter = strconv.FormatInt(selected[index].Seq, 10)
					break
				}
			}
		}
	}

	page := &server.ConversationTurnContextPage{
		Turns:         selected,
		NextBefore:    nextBefore,
		NextAfter:     nextAfter,
		HasMoreBefore: hasMoreBefore,
		HasMoreAfter:  hasMoreAfter,
	}
	if len(selected) == 0 {
		page.Events = []domain.ConversationEvent{}
		return page, nil
	}
	eventItems, err := events.ListConversationEventsByTurnSeqRange(ctx, conversationID, selected[0].Seq, selected[len(selected)-1].Seq)
	if err != nil {
		return nil, err
	}
	page.Events = eventItems
	return page, nil
}
