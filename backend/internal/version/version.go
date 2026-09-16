package version

var (
	Version = "1.20.5"
	Repo    = "4kercc/CLICD"
)

func Current() string {
	if Version == "" {
		return "1.20.5"
	}
	return Version
}
