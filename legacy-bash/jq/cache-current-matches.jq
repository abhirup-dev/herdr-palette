# Exit success only when this cache pointer belongs to the installed Herdr CLI.
# Match both CLI version and index representation. A palette upgrade which
# adds cache fields causes exactly one automatic rebuild.
include "herdr-lib";
(.herdr_version == $herdr_version) and (.index_format == index_format)
