package plugins

import "errors"

const pluginIsolationNotImplementedMessage = "plugins.isolation.enabled is not implemented yet"

var errPluginIsolationNotImplemented = errors.New(pluginIsolationNotImplementedMessage)
