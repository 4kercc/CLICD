package version

var (
	Version = "1.20.13"
	Repo    = "4kercc/CLICD"
)

func Current() string {
	if Version == "" {
		return "1.20.13"
	}
	return Version
}
