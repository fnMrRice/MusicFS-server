# MusicFS Server

[简体中文](../README.md) · English

MusicFS Server is a self-hosted music library. It scans directories on the server, organizes versions of the same track alongside lyrics and artwork, and streams audio over HTTP to Web and native clients.

- **Organize a library.** Browse tracks, albums, artists, or folders, and manage favorites and playlists. You can also import folders containing CD albums, ASMR, or radio programs.
- **Fill in metadata.** Read embedded lyrics and artwork or import them from sidecar files. Preview online results and choose which fields to save. [Metadata and online provider references](plugins/provider-api-sources.md)
- **Stream audio.** Send the original stream when the client supports its format, or transcode with FFmpeg. The Bilibili provider accepts links to public audio, favorites, and audio playlists. Other platforms offer different account and metadata features. [Online providers](providers/online.md)

## Run locally

Run the following commands from the Server repository root with the Go 1.26.9 toolchain specified in [go.mod](../go.mod). This Windows PowerShell example starts the API on the loopback interface.

```powershell
./scripts/build.ps1
Copy-Item configs/server.example.yaml configs/server.local.yaml
$env:MUSICFS_ADMIN_TOKEN = (& ./build/windows/musicfs.exe token).Trim()
./build/windows/musicfs.exe serve --config ./configs/server.local.yaml
```

Open the [readiness endpoint](http://127.0.0.1:8080/api/v1/health/ready) to check that the server is ready. For the browser interface, first build [MusicFS Web](https://github.com/fnMrRice/MusicFS-web), then add `--web-root <absolute-path-to-dist>` to the `serve` command. To see the generated login token, enter `$env:MUSICFS_ADMIN_TOKEN` before starting the service. Sign in and add a directory under Settings → Music Sources → Local Music to start a scan. The [local setup guide](operations/running.md) covers Linux, directory permissions, and FFmpeg configuration.

## Documentation

| Task | Guides |
| --- | --- |
| Deploy, configure, or back up the service | [Deployment](operations/deployment.md) · [Configuration](operations/configuration.md) · [Backup and restore](operations/backup.md) |
| Connect a client | [OpenAPI and events](contracts/README.md) · [Web integration](contracts/web-integration.md) |
| Work on the server or a plugin | [Development and testing](development/README.md) · [Source guide](../src/README.md) · [Plugin SDK](../sdk/README.md) |
| Explore the architecture and supported features | [Server architecture](architecture.md) · [Validation status](validation/README.md) · [Documentation index](README.md) |

This project was generated almost entirely by AI.

[MusicFS project and screenshots](https://github.com/fnMrRice/MusicFS) · [License](../LICENSE)
