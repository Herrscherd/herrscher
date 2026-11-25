package main

func cacheError(cause error) error {
	return messageError{key: msgErrCache, cause: cause}
}

func sandboxError(cause error) error {
	return messageError{key: msgErrSandbox, cause: cause}
}

func vaultError(cause error) error {
	return messageError{key: msgErrVault, cause: cause}
}

func sweepError(cause error) error {
	return messageError{key: msgErrSweep, cause: cause}
}

func reportError(cause error) error {
	return messageError{key: msgErrReport, cause: cause}
}

func pathError(key msgKey, path string, cause error) error {
	return messageError{key: key, params: params{paramPath: path}, cause: cause}
}
