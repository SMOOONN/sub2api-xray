package domain

// GroupRetryFilterConfig stores the optional retry-filter preset reference.
type GroupRetryFilterConfig struct {
	PresetID *int64 `json:"preset_id,omitempty"`
}
