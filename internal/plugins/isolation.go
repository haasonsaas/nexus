package plugins

import "errors"

const pluginIsolationNotImplementedMessage = "plugins.isolation.enabled is not implemented yet (issue #97)"

var errPluginIsolationNotImplemented = errors.New(pluginIsolationNotImplementedMessage)
