package config

// K8s manifest interface implementation for ScenarioConfig
func (c *ScenarioConfig) GetAPIVersion() string {
	return c.APIVersion
}

func (c *ScenarioConfig) GetKind() string {
	return c.Kind
}

func (c *ScenarioConfig) GetName() string {
	return c.Metadata.Name
}

func (c *ScenarioConfig) SetID(id string) {
	c.Spec.ID = id
}

// K8s manifest interface implementation for ProviderConfig
func (c *ProviderConfig) GetAPIVersion() string {
	return c.APIVersion
}

func (c *ProviderConfig) GetKind() string {
	return c.Kind
}

func (c *ProviderConfig) GetName() string {
	return c.Metadata.Name
}

func (c *ProviderConfig) SetID(id string) {
	c.Spec.ID = id
}
