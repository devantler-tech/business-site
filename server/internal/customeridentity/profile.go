package customeridentity

// Profile is the directory label for a person. It carries no access grants.
type Profile struct {
	Name          string
	Email         string
	EmailVerified bool
}
