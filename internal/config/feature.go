package config

// FeatureGates represents K9s opt-in features.
type FeatureGates struct {
	NodeShell           bool `yaml:"nodeShell"`
	AutoRemoveNodeShell bool `yaml:"autoRemoveNodeShell"`
}

// NewFeatureGates returns a new feature gate.
func NewFeatureGates() *FeatureGates {
	return &FeatureGates{
		NodeShell:           true,
		AutoRemoveNodeShell: true,
	}
}
