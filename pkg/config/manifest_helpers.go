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

// K8s manifest interface implementation for ScenarioConfigK8s
func (c *ScenarioConfigK8s) GetAPIVersion() string {
	return c.APIVersion
}

func (c *ScenarioConfigK8s) GetKind() string {
	return c.Kind
}

func (c *ScenarioConfigK8s) GetName() string {
	return c.Metadata.Name
}

func (c *ScenarioConfigK8s) SetID(id string) {
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

// GetAPIVersion returns the API version for EvalConfig
func (c *EvalConfig) GetAPIVersion() string {
	return c.APIVersion
}

// GetKind returns the kind for EvalConfig
func (c *EvalConfig) GetKind() string {
	return c.Kind
}

// GetName returns the metadata name for EvalConfig
func (c *EvalConfig) GetName() string {
	return c.Metadata.Name
}

// SetID sets the ID in the spec for EvalConfig
func (c *EvalConfig) SetID(id string) {
	c.Spec.ID = id
}

// GetAPIVersion returns the API version for EvalConfigK8s
func (c *EvalConfigK8s) GetAPIVersion() string {
	return c.APIVersion
}

// GetKind returns the kind for EvalConfigK8s
func (c *EvalConfigK8s) GetKind() string {
	return c.Kind
}

// GetName returns the metadata name for EvalConfigK8s
func (c *EvalConfigK8s) GetName() string {
	return c.Metadata.Name
}

// SetID sets the ID in the spec for EvalConfigK8s
func (c *EvalConfigK8s) SetID(id string) {
	c.Spec.ID = id
}
