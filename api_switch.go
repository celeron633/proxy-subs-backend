package main

type ApiSwitch struct {
	EnableFlag bool
}

func (apiSwitch *ApiSwitch) Enable() {
	apiSwitch.EnableFlag = true
}

func (apiSwitch *ApiSwitch) Disable() {
	apiSwitch.EnableFlag = false
}

func (apiSwitch *ApiSwitch) IsEnabled() bool {
	return apiSwitch.EnableFlag
}

func NewApiSwitch(enable bool) *ApiSwitch {
	return &ApiSwitch{
		EnableFlag: enable,
	}
}
