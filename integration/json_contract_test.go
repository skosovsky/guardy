package integration_test

type schemaContractArgs struct {
	Profile struct {
		Role string `json:"role,omitempty"`
	} `json:"profile"`
}
