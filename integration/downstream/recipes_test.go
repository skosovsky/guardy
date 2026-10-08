package downstream_test

type disclosureFacts struct {
	Recipient, Session, Trust string
	Authorized                bool
}
type restoreArgs struct {
	Token string `json:"token"`
	Claim string `json:"claim"`
}

// This is a single-threaded host recipe, not an IAM or distributed vault API.
// The host selects the session vault and checks expiry/recipient before lookup.
//
