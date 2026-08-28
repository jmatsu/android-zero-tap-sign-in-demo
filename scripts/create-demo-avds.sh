#!/usr/bin/env bash
#
# Creates the two emulators this demo needs: one to sign in on and back up, one
# to restore onto.
#
# The system image has to be a Google Play image. Restore Credentials lives in
# Google Play services and needs GMS core 24220000 or newer, and only the
# google_apis_playstore images can update GMS through the Play Store.
#
# Usage: scripts/create-demo-avds.sh [api_level]

set -euo pipefail

API_LEVEL="${1:-35}"
DEVICE_PROFILE="${ZEROTAP_AVD_DEVICE:-pixel_9_pro}"
AVD_NAMES=("zerotap-a" "zerotap-b")

SDK_ROOT="${ANDROID_HOME:-${ANDROID_SDK_ROOT:-$HOME/Library/Android/sdk}}"
SDKMANAGER="$SDK_ROOT/cmdline-tools/latest/bin/sdkmanager"
AVDMANAGER="$SDK_ROOT/cmdline-tools/latest/bin/avdmanager"

for tool in "$SDKMANAGER" "$AVDMANAGER"; do
    if [ ! -x "$tool" ]; then
        echo "error: $tool not found. Install the SDK command-line tools:" >&2
        echo "       Android Studio → SDK Manager → SDK Tools → Android SDK Command-line Tools" >&2
        exit 1
    fi
done

case "$(uname -m)" in
    arm64|aarch64) ABI="arm64-v8a" ;;
    *)             ABI="x86_64" ;;
esac

PACKAGE="system-images;android-${API_LEVEL};google_apis_playstore;${ABI}"

echo "==> Installing $PACKAGE"
"$SDKMANAGER" --install "$PACKAGE"

# An AVD can live in more than one place, and the tools do not agree on which
# one wins: ANDROID_AVD_HOME, $ANDROID_USER_HOME/avd, the legacy
# $ANDROID_SDK_HOME/.android/avd, and $HOME/.android/avd are all candidates.
# Android Studio and the command-line emulator can end up launching different
# copies of the same name, so tune every copy we can find.
avd_homes() {
    {
        [ -n "${ANDROID_AVD_HOME:-}" ] && echo "$ANDROID_AVD_HOME"
        [ -n "${ANDROID_USER_HOME:-}" ] && echo "$ANDROID_USER_HOME/avd"
        [ -n "${ANDROID_SDK_HOME:-}" ] && echo "$ANDROID_SDK_HOME/.android/avd"
        echo "$HOME/.android/avd"
    } | awk 'NF && !seen[$0]++'
}

set_config() {
    local config="$1" key="$2" value="$3"
    if grep -q "^${key}=" "$config"; then
        sed -i.bak "s|^${key}=.*|${key}=${value}|" "$config" && rm -f "$config.bak"
    else
        echo "${key}=${value}" >> "$config"
    fi
}

tune() {
    local config="$1"
    # avdmanager leaves the Play Store app disabled even on a Play Store image.
    # It has to be on, because updating Google Play services through the Play
    # Store is the only way to reach the GMS version Restore Credentials needs.
    set_config "$config" "PlayStore.enabled" "yes"
    # The 2G default is tight once a backup and a restore are both on disk.
    set_config "$config" "disk.dataPartition.size" "6G"
    # Without this the emulator ignores the host keyboard, so text fields look
    # focused but swallow everything you type. avdmanager leaves it unset, which
    # the emulator reads as "no"; Android Studio sets it for you.
    set_config "$config" "hw.keyboard" "yes"
}

# One listing for the whole loop; each invocation is a JVM start.
existing="$("$AVDMANAGER" list avd -c)"

for name in "${AVD_NAMES[@]}"; do
    if grep -qx "$name" <<<"$existing"; then
        echo "==> $name already exists, re-applying its configuration"
    else
        echo "==> Creating $name"
        echo "no" | "$AVDMANAGER" create avd \
            --name "$name" \
            --package "$PACKAGE" \
            --device "$DEVICE_PROFILE"
    fi

    found=0
    while IFS= read -r home; do
        config="$home/${name}.avd/config.ini"
        [ -f "$config" ] || continue
        tune "$config"
        found=$((found + 1))
        echo "    $config"
    done < <(avd_homes)

    if [ "$found" -eq 0 ]; then
        echo "warning: could not locate a config.ini for $name; it may need tuning by hand" >&2
    elif [ "$found" -gt 1 ]; then
        echo "warning: $name exists in $found locations listed above. Android Studio and the" >&2
        echo "         command-line emulator may launch different ones. Delete the copies you" >&2
        echo "         do not want, keeping whichever your tools actually use." >&2
    fi
done

echo
echo "Done. Start them with:"
for name in "${AVD_NAMES[@]}"; do
    echo "  \$ANDROID_HOME/emulator/emulator -avd $name"
done
echo
echo "Then on each one, before testing the transfer:"
echo "  - sign in to a Google account"
echo "  - Settings > Security > Screen lock > PIN"
echo "  - Settings > Google > Backup > on"
echo "  - open the Play Store once so Play services updates itself"
echo
echo "Delete them again with:"
for name in "${AVD_NAMES[@]}"; do
    echo "  $AVDMANAGER delete avd --name $name"
done
