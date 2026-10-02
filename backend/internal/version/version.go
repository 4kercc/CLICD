package version

var (
	Version = "1.20.22"
	Repo    = "4kercc/CLICD"
)

func Current() string {
	if Version == "" {
		return "1.20.22"
	}
	return Version
}
