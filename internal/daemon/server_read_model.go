package daemon

import (
	"context"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/telemetry"
)

func (s *Service) computeReadModel(
	ctx context.Context,
	req ReadModelRequest,
) (map[string]core.UsageSnapshot, error) {
	start := time.Now()
	templates := ReadModelTemplatesFromRequest(req, DisabledAccountsFromConfig())
	if len(templates) == 0 {
		return map[string]core.UsageSnapshot{}, nil
	}
	tw := normalizeReadModelTimeWindow(req.TimeWindow)
	result, err := telemetry.ApplyCanonicalTelemetryViewWithOptions(ctx, s.cfg.DBPath, templates, telemetry.ReadModelOptions{
		ProviderLinks: req.ProviderLinks,
		Since:         tw.Since(),
		TodaySince:    core.LocalMidnight(),
		TimeWindow:    tw,
	})
	core.Tracef("[read_model_perf] computeReadModel TOTAL: %dms (window=%s, accounts=%d, results=%d)",
		time.Since(start).Milliseconds(), tw, len(req.Accounts), len(result))
	return result, err
}

func shouldRefreshCachedReadModel(cachedAt time.Time, cachedVersion, currentVersion uint64, now time.Time) bool {
	return currentVersion > cachedVersion && now.Sub(cachedAt) > 2*time.Second
}

func (s *Service) refreshReadModelCacheAsync(
	parent context.Context,
	cacheKey string,
	req ReadModelRequest,
	timeout time.Duration,
) {
	if !s.rmCache.beginRefresh(cacheKey) {
		return
	}
	refreshVersion := s.dataVersion.Load()
	go func() {
		defer s.rmCache.endRefresh(cacheKey)
		refreshCtx, cancel := context.WithTimeout(parent, timeout)
		defer cancel()
		snapshots, err := s.computeReadModel(refreshCtx, req)
		if err != nil {
			if s.shouldLog("read_model_cache_refresh_error", 8*time.Second) {
				s.warnf("read_model_cache_refresh_error", "error=%v", err)
			}
			return
		}
		s.rmCache.set(cacheKey, snapshots, refreshVersion)
	}()
}

func (s *Service) serviceContext(fallback context.Context) context.Context {
	if s != nil && s.ctx != nil {
		return s.ctx
	}
	if fallback != nil {
		return fallback
	}
	return context.Background()
}
