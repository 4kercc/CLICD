package version

var (
	Version = "1.20.16"
	Repo    = "4kercc/CLICD"
)

func Current() string {
	if Version == "" {
		return "1.20.16"
	}
	return Version
}
