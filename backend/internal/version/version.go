package version

var (
	Version = "1.1.29"
	Repo    = "MengMengCode/CLICD"
)

func Current() string {
	if Version == "" {
		return "dev"
	}
	return Version
}
