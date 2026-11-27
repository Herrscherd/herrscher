package drives

import (
	"fmt"
	"strings"
)

// Kind is the service a drive is stored on.
type Kind int

const (
	// KindGoogleDrive is Google Drive, signed in with OAuth.
	KindGoogleDrive Kind = iota + 1
	// KindDropbox is Dropbox, signed in with OAuth.
	KindDropbox
	// KindOneDrive is Microsoft OneDrive, signed in with OAuth.
	KindOneDrive
	// KindS3 is an S3 compatible object store, reached with an access key.
	KindS3
	// KindR2 is Cloudflare R2, reached with an access key.
	KindR2
)

const (
	kindGoogleDriveName = "gdrive"
	kindDropboxName     = "dropbox"
	kindOneDriveName    = "onedrive"
	kindS3Name          = "s3"
	kindR2Name          = "r2"
)

// UnknownKindError reports a drive kind herrscher does not connect.
type UnknownKindError struct {
	Value string
}

func (e *UnknownKindError) Error() string {
	return fmt.Sprintf("drive kind %q is none of %s", e.Value, strings.Join(KindNames(), ", "))
}

// KindNames lists the kinds as an operator writes them.
func KindNames() []string {
	return []string{kindGoogleDriveName, kindDropboxName, kindOneDriveName, kindS3Name, kindR2Name}
}

// ParseKind reads a drive kind as an operator writes it.
func ParseKind(raw string) (Kind, error) {
	switch raw {
	case kindGoogleDriveName:
		return KindGoogleDrive, nil
	case kindDropboxName:
		return KindDropbox, nil
	case kindOneDriveName:
		return KindOneDrive, nil
	case kindS3Name:
		return KindS3, nil
	case kindR2Name:
		return KindR2, nil
	}
	return 0, &UnknownKindError{Value: raw}
}

func (k Kind) String() string {
	switch k {
	case KindGoogleDrive:
		return kindGoogleDriveName
	case KindDropbox:
		return kindDropboxName
	case KindOneDrive:
		return kindOneDriveName
	case KindS3:
		return kindS3Name
	case KindR2:
		return kindR2Name
	}
	return fmt.Sprintf("kind(%d)", int(k))
}

// SignsInWithOAuth reports whether the kind is connected through a browser sign in rather than a key.
func (k Kind) SignsInWithOAuth() bool {
	switch k {
	case KindGoogleDrive, KindDropbox, KindOneDrive:
		return true
	case KindS3, KindR2:
	}
	return false
}

// RcloneType is the rclone backend that serves the kind.
func (k Kind) RcloneType() string {
	switch k {
	case KindGoogleDrive:
		return "drive"
	case KindDropbox:
		return kindDropboxName
	case KindOneDrive:
		return kindOneDriveName
	case KindS3, KindR2:
		return kindS3Name
	}
	return ""
}

// MarshalText writes the kind as its name.
func (k Kind) MarshalText() ([]byte, error) {
	return []byte(k.String()), nil
}

// UnmarshalText reads the kind from its name.
func (k *Kind) UnmarshalText(text []byte) error {
	parsed, err := ParseKind(string(text))
	if err != nil {
		return err
	}
	*k = parsed
	return nil
}
