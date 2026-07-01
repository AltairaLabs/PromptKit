package config

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

// K8s manifest interface implementation for ProviderConfigK8s
func (c *ProviderConfigK8s) GetAPIVersion() string {
	return c.APIVersion
}

func (c *ProviderConfigK8s) GetKind() string {
	return c.Kind
}

func (c *ProviderConfigK8s) GetName() string {
	return c.Metadata.Name
}

func (c *ProviderConfigK8s) SetID(id string) {
	c.Spec.ID = id
}
