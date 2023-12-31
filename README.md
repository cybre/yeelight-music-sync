# Yeelight Music Sync

`cmd/controller` contains a small CLI that listens to an audio input device, analyses the signal in real time, and drives a Yeelight bulb (in music mode) so the lamp mirrors the music. It is designed for setups where you want a light to pulse, shimmer, and change colour with the beat.

## How It Works

1. **Audio Capture** – PortAudio streams audio frames from a selected input (microphone, loopback, etc.).
2. **Analysis** – `internal/dsp` computes per‑band energy, spectral centroid, rolloff and beat intensity. `internal/patterns` converts those features into lighting "states".
3. **Bulb Control** – The first discovered Yeelight is connected over TCP and switched into music mode; LED colours are updated in real time based on the current pattern.
4. **Optional Visualiser** – `--visualize` draws a colourful terminal dashboard that previews the hue/brightness and analytics without needing to look at the lamp.

## Prerequisites

- Go ≥ 1.23
- A Yeelight bulb that supports music mode and is reachable on the local network
- PortAudio runtime (macOS/Linux packages or Windows DLLs)

## Running It

```bash
go run ./cmd/controller --list-bulbs
go run ./cmd/controller --list-devices
```

1. Use `--list-bulbs` once to find the id of the Yeelight bulb you want to controll.
2. Use `--list-devices` once to find the index of the audio input you want to analyse.
3. Start the controller with your chosen bulb and audio device:

```bash
go run ./cmd/controller --bulb 0x0000000000000001 --device 3 --visualize
```

### Flags

| Flag | Description |
| ---- | ----------- |
| `--bulb` | Yeelight bulb ID (default: first discovered bulb) |
| `--list-bulbs` | Enumerate discovered Yeelight bulbs and exit |
| `--device` | Audio input index (default: system default input) |
| `--list-devices` | Enumerate PortAudio devices and exit |
| `--sample-rate` | Override capture sample rate (default: device default) |
| `--frame-size` | FFT frame size (default: 1024 samples) |
| `--channels` | Number of channels to capture (default: 2) |
| `--latency-ms` | Force input latency in ms (default: device default) |
| `--visualize` | Render the ANSI visualiser instead of normal logs |
| `--debug` | Emit verbose debug logs |

> When `--visualize` is enabled, informational logs are silenced so the terminal stays clean; use `--debug` alongside it if you still want detailed logging.

## Behaviour Notes

- The controller automatically toggles the bulb on if it is off, and falls back gracefully if music mode cannot be enabled.
- If you lose the audio stream (device unplugged, context cancelled) the program shuts down cleanly.

## Building

```bash
go build -o yeelight-controller ./cmd/controller
```

You can then run the compiled binary with the same flags as above.

## Troubleshooting

- **No devices listed** – Ensure PortAudio is installed and your user has permission to access the audio subsystem.
- **Bulb not found** – The Yeelight must respond to SSDP discovery on the same network segment. Confirm you can control it with the official app.
- **Laggy response** – Experiment with lower `--frame-size` and `--latency-ms` values; they trade off CPU usage and responsiveness.

Enjoy the light show! 🎶💡
