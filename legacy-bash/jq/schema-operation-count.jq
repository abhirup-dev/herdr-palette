# Independently count raw request operation branches in a Herdr protocol schema.
[.schemas.request.oneOf[] | select(.properties.method.const? and .properties.params."$ref"?)] | length
