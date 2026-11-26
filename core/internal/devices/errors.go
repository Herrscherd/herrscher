package devices

import "errors"

// ErrMalformedDeviceID reports an identifier NewDeviceID could not have made.
var ErrMalformedDeviceID = errors.New("malformed device id")

// ErrMalformedCode reports a pairing code that is not ten characters of the code alphabet.
var ErrMalformedCode = errors.New("malformed pairing code")

// ErrUngrantableRole reports a role that a device may not be given.
var ErrUngrantableRole = errors.New("a device can only be given a role herrscher grants")

// ErrPairingOpen reports a pairing window that is already open and not yet expired.
var ErrPairingOpen = errors.New("a pairing window is already open")

// ErrNoPairingWindow reports a pairing attempt when no window is open.
var ErrNoPairingWindow = errors.New("no pairing window is open")

// ErrPairingExpired reports a pairing code presented after its window closed by time.
var ErrPairingExpired = errors.New("the pairing code has expired")

// ErrPairingAlreadyRedeemed reports a pairing code presented again after a request already came in with it.
var ErrPairingAlreadyRedeemed = errors.New("the pairing code was already redeemed")

// ErrNothingPending reports an approval when no device has redeemed the code.
var ErrNothingPending = errors.New("no device is waiting for approval")

// ErrWrongCode reports a pairing code that does not match the open window.
var ErrWrongCode = errors.New("the pairing code is wrong")

// ErrTooManyAttempts reports the wrong code that closed the window.
var ErrTooManyAttempts = errors.New("too many wrong pairing codes: the window is closed")

// ErrBadRequest reports a certificate request whose signature does not check out.
var ErrBadRequest = errors.New("the certificate request is not valid")

// ErrUnsupportedKey reports a device key that is not ECDSA on P-256.
var ErrUnsupportedKey = errors.New("the device key must be an ECDSA P-256 key")

// ErrUnknownDevice reports a certificate naming a device the registry does not hold.
var ErrUnknownDevice = errors.New("unknown device")

// ErrDeviceRevoked reports a device an operator revoked.
var ErrDeviceRevoked = errors.New("the device is revoked")

// ErrDeviceExpired reports a device that has not connected for thirty days.
var ErrDeviceExpired = errors.New("the device has not connected for thirty days")

// ErrCertificateExpired reports a client certificate presented outside its validity period.
var ErrCertificateExpired = errors.New("the client certificate is outside its validity period")

// ErrCertificateMismatch reports a client certificate whose serial or key differs from the device record.
var ErrCertificateMismatch = errors.New("the client certificate does not match the device record")

// ErrUntrustedCertificate reports a client certificate that the daemon's authority did not issue.
var ErrUntrustedCertificate = errors.New("the client certificate was not issued by this daemon")

// ErrAuthorityExpired reports a stored authority certificate past its validity.
var ErrAuthorityExpired = errors.New("the device authority certificate has expired")

// ErrAuthorityFiles reports a device authority whose files are missing, partial or unreadable.
var ErrAuthorityFiles = errors.New("the device authority files are missing or unreadable")

// ErrRandomSource reports that the operating system's random source failed.
var ErrRandomSource = errors.New("the operating system's random source failed")

// ErrSigning reports that the authority could not sign a certificate.
var ErrSigning = errors.New("the device certificate could not be signed")

// ErrUnsupportedSignature reports a certificate request signed with an algorithm other than ECDSA over SHA-256 or SHA-384.
var ErrUnsupportedSignature = errors.New("the certificate request must be signed with ECDSA and SHA-256 or SHA-384")

// ErrMalformedFingerprint reports a short fingerprint that is not eight symbols of the pairing alphabet.
var ErrMalformedFingerprint = errors.New("malformed fingerprint")

// ErrFingerprintMismatch reports a fingerprint that does not match the key the device sent.
var ErrFingerprintMismatch = errors.New("the fingerprint does not match the one the device shows: nothing was signed")

// ErrAuthorityMissing reports that the device authority is gone while devices are paired, so nothing is rebuilt.
var ErrAuthorityMissing = errors.New("the device authority is missing although devices are paired: nothing was rebuilt")

// ErrRequestTooLarge reports a certificate request larger than any device request can legitimately be.
var ErrRequestTooLarge = errors.New("the certificate request is too large")
