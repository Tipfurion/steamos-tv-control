# SteamOS TV Control

English · [Russian](README.ru.md)

Make your SteamOS PC feel more like a console: your TV turns on when the PC
starts or wakes, and goes into standby when the PC sleeps or shuts down.
Restarting the PC leaves the TV on.

Works with TVs that have Google TV or Android TV built in. Tested on a TCL
65C7L. Your PC and TV must be connected to the same home network.

## Before you start

Turn on the TV and connect the PC to an HDMI port.

On TCL, enable **higher energy use**, **Screenless service**, or the option that
keeps the TV available while its screen is off. The name depends on the model.
On the tested 65C7L, higher energy use enabled waking the TV over the network.

## Install

Switch SteamOS to **Desktop Mode**, open **Konsole**, and paste:

```bash
curl -fsSL https://github.com/Tipfurion/steamos-tv-control/releases/latest/download/install.sh | bash
```

This installs the latest published version. For a specific version, use the
installation command on its [release page](https://github.com/Tipfurion/steamos-tv-control/releases).

The command finds your TV and sets everything up. Enter the code shown on the
TV, then select the HDMI port connected to your PC. If more than one TV is
found, choose yours from the list. No setup wizard is opened.
Confirm that your PC picture is visible when asked.

If your PC is not connected to the TV yet, press Enter at the HDMI question.
Automatic power control will still be configured. You can set up HDMI later.

SteamOS may ask for your password to allow automatic startup and TV control
before the PC sleeps. When installation finishes, return to Gaming Mode and
use your PC normally. The SteamOS system image stays read-only.

## Advanced settings

To change the TV, adjust HDMI switching, or turn automatic standby on or off:

```bash
~/.local/bin/steamos-tv-control setup
```

Follow the hints on screen. Press `q` to close the settings interface.

## Update or remove

To update, run the installation command again. Your settings are kept.

To remove the application:

```bash
~/.local/bin/steamos-tv-control uninstall
```

To remove it together with its saved settings:

```bash
~/.local/bin/steamos-tv-control uninstall --purge
```

## If something does not work

Make sure the TV is on the same home network and remains available when its
screen is off. On TCL, check the energy setting mentioned above. Use a short
press on the TV remote to enter standby; a full shutdown prevents network wake.

To see what the application is doing, paste:

```bash
journalctl --user -u steamos-tv-control -f
```

Press Ctrl+C to stop viewing the log.
