//go:build darwin && cgo

#import <CoreServices/CoreServices.h>
#import <Foundation/Foundation.h>

#include "appkit_darwin.h"
#include "files_darwin.h"

/* fd_url is path as a file URL, or nil for an empty one, which NSURL refuses. */
static NSURL *fd_url(const char *path) {
	NSString *p = fd_string(path);
	return p.length > 0 ? [NSURL fileURLWithPath:p] : nil;
}

/* fd_copy_value copies a dictionary's string value into a C buffer, and an
 * empty string for anything else. */
static void fd_copy_value(char *dst, int size, NSDictionary *d, CFStringRef key) {
	id value = d[(__bridge NSString *)key];
	fd_copy_c(dst, (size_t)size, [value isKindOfClass:NSString.class] ? value : @"");
}

int fd_quarantine(const char *path, char *err, int errlen) {
	@autoreleasepool {
		NSURL *url = fd_url(path);
		/* What a browser records about a download: what kind it is, which
		 * program saved it, and when. The system reads it when the file is
		 * opened: an app or an installer from elsewhere is checked, and the
		 * operator asked, before anything of it runs. */
		NSDictionary *properties = @{
			(__bridge NSString *)kLSQuarantineTypeKey : (__bridge NSString *)kLSQuarantineTypeOtherDownload,
			(__bridge NSString *)kLSQuarantineAgentNameKey : @"FreeDesk",
			(__bridge NSString *)kLSQuarantineTimeStampKey : [NSDate date],
		};
		NSError *error = nil;
		if (!url || ![url setResourceValue:properties forKey:NSURLQuarantinePropertiesKey error:&error]) {
			fd_copy_c(err, (size_t)errlen, error.localizedDescription ?: @"the file could not be marked");
			return 0;
		}
		return 1;
	}
}

int fd_quarantine_read(const char *path, char *type, int typelen, char *agent, int agentlen) {
	@autoreleasepool {
		NSDictionary *properties = nil;
		if (![fd_url(path) getResourceValue:&properties forKey:NSURLQuarantinePropertiesKey error:NULL] ||
		    ![properties isKindOfClass:NSDictionary.class]) {
			return 0;
		}
		fd_copy_value(type, typelen, properties, kLSQuarantineTypeKey);
		fd_copy_value(agent, agentlen, properties, kLSQuarantineAgentNameKey);
		return 1;
	}
}

int fd_volume_capacity(const char *path, uint64_t *bytes) {
	@autoreleasepool {
		/* The capacity for important use: a file the operator agreed to take
		 * is one. It counts what macOS frees when it needs the room (caches,
		 * local Time Machine snapshots, files also kept in iCloud), as the
		 * "available" the Finder shows does; the file system's own free count
		 * leaves that out, and can be tens of gigabytes short. */
		NSNumber *capacity = nil;
		if (![fd_url(path) getResourceValue:&capacity forKey:NSURLVolumeAvailableCapacityForImportantUsageKey error:NULL] ||
		    ![capacity isKindOfClass:NSNumber.class] || capacity.longLongValue < 0) {
			return 0;
		}
		*bytes = capacity.unsignedLongLongValue;
		return 1;
	}
}
