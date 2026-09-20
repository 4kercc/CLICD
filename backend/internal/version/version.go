package version

var (
	Version = "1.20.8"
	Repo    = "4kercc/CLICD"
)

func Current() string {
	if Version == "" {
		return "1.20.8"
	}
	return Version
}
