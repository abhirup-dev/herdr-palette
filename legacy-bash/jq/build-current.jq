# Construct the cache pointer after an index has been atomically installed.
include "herdr-lib";
{
  herdr_version: $herdr_version,
  protocol: $protocol,
  schema_version: $schema_version,
  cache: $cache,
  indexed_at: $indexed_at,
  index_format: index_format
}
