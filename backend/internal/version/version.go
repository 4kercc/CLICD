package version

var (
	Version = "1.20.14"
	Repo    = "4kercc/CLICD"
)

func Current() string {
	if Version == "" {
		return "1.20.14"
	}
	return Version
}
