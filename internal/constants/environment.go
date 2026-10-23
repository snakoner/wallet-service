package constants

type Environment string

const (
	EnvironmentIntegration Environment = "integration"
	EnvironmentDevelopment Environment = "development"
	EnvironmentStaging     Environment = "staging"
	EnvironmentProduction  Environment = "production"
)
