package arbutil

import (
  "context"
  "github.com/ethereum/go-ethereum/log" 
)
const RpcKey = "rpc-usage"
// AddToCallstackContext
// Description: This function is a helper intended to add a callstack to a context key value pair for use with evaluating what is calling rpc related code.
// Return: A new context for use in sub-processes if the context already has a value for this path (meaning this needs to be added manually the first time to start the callstack)
// And a boolean representing whether the attempt to add to the context was successful.
func AddToCallstackContext(parent_ctx context.Context, callstack_addition string) (context.Context, bool){
  current_callstack, ok := parent_ctx.Value(RpcKey).(string)
  if current_callstack != "" && ok{
    new_callstack := current_callstack + callstack_addition
   return context.WithValue(parent_ctx, RpcKey, new_callstack), ok
  }
  return parent_ctx, ok
}

func LogCallstack(ctx context.Context){
  log.Info("Callstack for current rpc call", "callstack", ctx.Value(RpcKey))
}
