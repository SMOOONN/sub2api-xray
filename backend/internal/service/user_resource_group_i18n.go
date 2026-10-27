package service

import "encoding/json"

// normalizeUserResourceGroupI18n keeps the private-group adapter on the shared
// group translation contract. An omitted or null value preserves translations;
// an empty object explicitly clears them.
func normalizeUserResourceGroupI18n(payload map[string]any) error {
	value, present := payload["i18n"]
	if !present || value == nil {
		delete(payload, "i18n")
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return invalidUserResourceField("i18n", "must be an object of locale translations")
	}
	var translations GroupI18n
	if err := json.Unmarshal(raw, &translations); err != nil {
		return invalidUserResourceField("i18n", "must be an object of locale translations")
	}
	normalized, err := normalizeGroupI18n(translations)
	if err != nil {
		return err
	}
	payload["i18n"] = normalized
	return nil
}
