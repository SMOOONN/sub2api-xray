package model

import "time"

// RetryFilterPreset is a named collection of retry filter rules.
type RetryFilterPreset struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Enabled     bool      `json:"enabled"`
	Description *string   `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (p *RetryFilterPreset) Validate() error {
	if p == nil || p.Name == "" {
		return &ValidationError{Field: "name", Message: "name is required"}
	}
	return nil
}
