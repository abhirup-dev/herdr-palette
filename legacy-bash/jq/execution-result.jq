# Small accessors for execute.sh. Keeping JSON inspection here avoids inline jq
# programs in the shell dispatcher.
def action($name):
  if $name == "ok" then .ok
  elif $name == "error-message" then .error.message // .error.code // "render failed"
  elif $name == "argv-b64" then .argv[] | @base64
  elif $name == "review" then {method:$ARGS.named.method, answers:$ARGS.named.answers, argv:.argv}
  else error("unknown execution-result action: " + $name) end;
action($ARGS.named.action)
