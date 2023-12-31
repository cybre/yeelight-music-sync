# Yeelight Music Sync

`cmd/controller` contains a small CLI that listens to an audio input device, analyses the signal in real time, and drives a Yeelight bulb (in music mode) so the lamp mirrors the music. It is designed for setups where you want a light to pulse, shimmer, and change colour with the beat.

## How It Works

1. **Audio Capture** - PortAudio streams audio frames from a selected input (microphone, loopback, etc.).
2. **Analysis** - `internal/dsp` computes per‑band energy, spectral centroid, rolloff and beat intensity. `internal/patterns` converts those features into lighting "states".
3. **Bulb Control** - The first discovered Yeelight is connected over TCP and switched into music mode; LED colours are updated in real time based on the current pattern.
4. **Optional Visualiser** - `--visualize` launches a colourful dashboard that previews hue, brightness, and the analysed metrics without needing to look at the lamp. Exit with `q`, `esc`, or `ctrl+c`.

## Prerequisites

- Go ≥ 1.24
- A Yeelight bulb that supports music mode and is reachable on the local network
- PortAudio runtime (macOS/Linux packages or Windows DLLs)

## Running It

```bash
go run ./cmd/controller
```

1. Follow the interactive setup to choose a bulb and audio device (`↑/↓` or `j/k` to move, `enter` to confirm).
2. (Optional) pass `--bulb` or `--device` if you already know the address/index; providing `--bulb` skips SSDP discovery entirely.
3. Add `--visualize` to show the built-in terminal visualiser while the controller runs. Exit it anytime with `q`, `esc`, or `ctrl+c`.

   ![Terminal visualiser preview](screenshots/visualizer.png)

### Flags

| Flag | Description |
| ---- | ----------- |
| `--bulb` | Yeelight bulb address (otherwise choose interactively) |
| `--device` | Audio input index (otherwise choose interactively) |
| `--sample-rate` | Override capture sample rate (default: device default) |
| `--frame-size` | FFT frame size (default: 1024 samples) |
| `--channels` | Number of channels to capture (default: 2) |
| `--latency-ms` | Force input latency in ms (default: device default) |
| `--visualize` | Render the visualiser (quit with `q`/`esc`/`ctrl+c`) |
| `--debug` | Emit verbose debug logs (logs remain on stderr even with the visualiser) |

> When `--visualize` is enabled the UI takes over the terminal; logs are routed to stderr and are only shown when `--debug` is supplied.

## Behaviour Notes

- The controller automatically toggles the bulb on if it is off, and falls back gracefully if music mode cannot be enabled.
- If you lose the audio stream (device unplugged, context cancelled) the program shuts down cleanly.

## Building

```bash
go build -o yeelight-controller ./cmd/controller
```

You can then run the compiled binary with the same flags as above.

## Troubleshooting

- **No devices discovered** - Ensure PortAudio is installed and your user has permission to access the audio subsystem.
- **Bulb not found** - The Yeelight must respond to SSDP discovery on the same network segment. Confirm you can control it with the official app.
- **Laggy response** - Experiment with lower `--frame-size` and `--latency-ms` values; they trade off CPU usage and responsiveness.

Enjoy the light show! 🎶💡
