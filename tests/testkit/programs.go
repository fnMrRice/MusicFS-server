package testkit

// ProgramPackage keeps fixture builds aligned with the categorized plugin
// layout. Import paths also work when the source plugin is a separate module.
func ProgramPackage(name string) string {
	const root = "github.com/fnMrRice/MusicFS-server/"
	switch name {
	case "musicfs-plugin-local":
		return root + "plugins/source/localfiles/cmd/" + name
	case "musicfs-plugin-audio-ffmpeg":
		return root + "plugins/decoder/ffmpeg/cmd/" + name
	case "musicfs-plugin-ncm":
		return root + "plugins/decoder/ncm/cmd/" + name
	case "musicfs-plugin-scraper-local":
		return root + "plugins/scraper/local/cmd/" + name
	case "musicfs-plugin-storage-sqlite":
		return root + "plugins/database/sqlite/cmd/" + name
	case "musicfs-plugin-dsp-gain":
		return root + "plugins/dsp/gain/cmd/" + name
	case "musicfs-plugin-netease", "musicfs-plugin-qqmusic", "musicfs-plugin-applemusic", "musicfs-plugin-online":
		return root + "plugins/source/online/cmd/" + name
	default:
		return root + "src/app/" + name
	}
}
