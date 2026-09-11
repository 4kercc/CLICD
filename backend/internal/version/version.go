package version

var (
	Version = "1.20.1"
	Repo    = "4kercc/CLICD"
)

func Current() string {
	if Version == "" {
		return "dev"
	}
	return Version
}
