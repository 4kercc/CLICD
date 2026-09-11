package version

var (
	Version = "1.2.0"
	Repo    = "4kercc/CLICD"
)

func Current() string {
	if Version == "" {
		return "dev"
	}
	return Version
}
