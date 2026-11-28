package customeridentity

// Profile is the directory label for a person. It carries no access grants.
type Profile struct {
	Name          string
	Email         string
	EmailVerified bool
}

// AccountPageSize bounds both the provider response and the portal access lookup.
const AccountPageSize = 25

// Account pairs a provider identity with the directory label shown to operators.
type Account struct {
	ID string
	Profile
}
