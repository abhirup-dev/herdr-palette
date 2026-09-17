# Selection-history ranking for the Herdr palette.
#
# Input is JSONL from selections.jsonl; --rawfile catalog supplies the normal
# LABEL<TAB>COMMAND manifest. A record is {ts,label,command}. Session state is
# not stored here. Scores use use-count times a portable 24-hour decay
# (1 / (1 + age-hours / 24)); source.sh prints the top eight as ★ entries
# before the normal catalog.

# Parse forgivingly: a torn/manual bad JSONL line is ignored rather than making
# the palette unavailable.
def jsonl_records:
  split("\n") | map(try fromjson catch empty) | map(select(type == "object" and (.command? | type == "string") and (.ts? | type == "string")));

def catalog_rows($catalog):
  $catalog | split("\n") | map(select(length > 0))
  | map(split("\t") | select(length == 2) | {label:.[0], command:.[1]});

def selection_score($records; $command):
  [ $records[] | select(.command == $command)
    | (try fromdateiso8601 catch 0) as $then
    | (now - $then) as $seconds
    | if $seconds < 0 then 1 else (1 / (1 + ($seconds / 3600 / 24))) end
  ] | add // 0;

def ranked_rows($catalog):
  jsonl_records as $records
  | catalog_rows($catalog)
  | unique_by(.command)
  | map(. + {score: selection_score($records; .command)})
  | map(select(.score > 0))
  | sort_by(-.score, .label)
  | .[0:8]
  | .[]
  | [(.label + " (*)"), .command] | @tsv;
