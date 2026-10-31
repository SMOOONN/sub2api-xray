package routes

import "context"

// Xray refreshes retry-filter settings after a settings write. Extend the
// upstream route fixture with the same batch-read semantics as the repository.
func (r *mcpRouteSettingsRepo) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	for _, key := range keys {
		if value, ok := r.values[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}
