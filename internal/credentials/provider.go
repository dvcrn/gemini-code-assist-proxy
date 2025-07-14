package credentials

// CredentialsProvider defines the interface for managing OAuth credentials
type CredentialsProvider interface {
	// GetCredentials retrieves the current OAuth credentials
	GetCredentials() (*OAuthCredentials, error)

	// SaveCredentials persists the OAuth credentials
	SaveCredentials(creds *OAuthCredentials) error

	// RefreshToken handles token refresh using the refresh token
	RefreshToken() error

	// Name returns the name of the provider for logging
	Name() string
}
