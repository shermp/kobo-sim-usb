/* 
Shamelessly pilfering this from FBInk. Functions renamed to avoid clashing with FBIink versions 

    FBInk: FrameBuffer eInker, a tool to print text & images on eInk devices (Kobo/Kindle)
	Copyright (C) 2018 NiLuJe <ninuje@gmail.com>
	----
	This program is free software: you can redistribute it and/or modify
	it under the terms of the GNU Affero General Public License as
	published by the Free Software Foundation, either version 3 of the
	License, or (at your option) any later version.
	This program is distributed in the hope that it will be useful,
	but WITHOUT ANY WARRANTY; without even the implied warranty of
	MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
	GNU Affero General Public License for more details.
	You should have received a copy of the GNU Affero General Public License
	along with this program.  If not, see <http://www.gnu.org/licenses/>.
*/

#include <stdlib.h>
#include <stdbool.h>
#include <stdint.h>
#include <stdio.h>
#include <mntent.h>
#include <poll.h>
#include <fcntl.h>
#include <string.h>
#include <unistd.h>

// Mountpoint monitoring helpers pilfered from KFMon ;).
// Check if onboard (the mountpoint, not the fs) is mounted or not...
static bool
    simusb_is_onboard_state(bool mounted, const char* mount_point)
{
	// c.f., http://program-nix.blogspot.com/2008/08/c-language-check-filesystem-is-mounted.html
	FILE*          mtab       = NULL;
	struct mntent* part       = NULL;
	bool           is_mounted = false;

	if ((mtab = setmntent("/proc/mounts", "r")) != NULL) {
		while ((part = getmntent(mtab)) != NULL) {
			if ((part->mnt_dir != NULL) && (strcmp(part->mnt_dir, mount_point)) == 0) {
				printf("[simusb] fs %s is mounted on %s", part->mnt_fsname, part->mnt_dir);
				is_mounted = true;
				break;
			}
		}
		endmntent(mtab);
	}

	// Return the right thing depending on which state we want onboard to be...
	if (mounted) {
		return is_mounted;
	} else {
		return !is_mounted;
	}
}

// Monitor mountpoint activity...
static bool
    sim_usb_wait_for_onboard_state(bool mounted, const char* mount_point)
{
	// c.f., https://stackoverflow.com/questions/5070801
	int           mfd = open("/proc/mounts", O_RDONLY, 0);
	struct pollfd pfd;

	uint8_t changes     = 0;
	uint8_t max_changes = 5;
	pfd.fd              = mfd;
	pfd.events          = POLLERR | POLLPRI;
	pfd.revents         = 0;
	// Assume success unless proven otherwise ;).
	bool rb = true;

	// NOTE: Abort early if the mountpoint is already in the requested state,
	//       in an effort to keep the race window as short as possible...
	if (simusb_is_onboard_state(mounted, mount_point)) {
		goto cleanup;
	}

	// NOTE: We're going with no timeout, which works out great when everything behaves as expected,
	//       but *might* be problematic in case something goes awfully wrong,
	//       in which case we might block for a while...
	while (poll(&pfd, 1, -1) >= 0) {
		if (pfd.revents & POLLERR) {
			printf("[simusb] Mountpoints changed (iteration nr. %hhu of %hhu)", ++changes, max_changes);
			// Stop polling once we know onboard is in the requested state...
			if (simusb_is_onboard_state(mounted, mount_point)) {
				printf("[simusb] Good, onboard is finally %s!", mounted ? "mounted" : "unmounted");
				break;
			}
		}
		pfd.revents = 0;

		// If we can't find our mountpoint after that many changes, assume we're screwed...
		if (changes >= max_changes) {
			printf("[simusb] Too many mountpoint changes without finding onboard, aborting!");
			rb = false;
			break;
		}
	}

cleanup:
	close(mfd);
	return rb;
}