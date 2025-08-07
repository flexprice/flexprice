package config

type PermitConfig struct {
	APIKey      string `mapstructure:"api_key" json:"api_key"`
	APIURL      string `mapstructure:"api_url" json:"api_url" default:"https://api.permit.io"`
	PDPURL      string `mapstructure:"pdp_url" json:"pdp_url"`
	Environment string `mapstructure:"environment" json:"environment" default:"development"`
	Debug       bool   `mapstructure:"debug" json:"debug" default:"false"`
	ProjectID   string `mapstructure:"project_id" json:"project_id"`
}

func (c *Configuration) GetPermitConfig() *PermitConfig {
	return &c.Permit
}
