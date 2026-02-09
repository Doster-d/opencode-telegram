import type { Plugin } from "@opencode-ai/plugin"

export const NoSelfDispatch: Plugin = async () => {
  return {
    "tool.execute.before": async (input, output) => {
      if (input.tool === "task" && output.args?.subagent_type === "orchestrator") {
        throw new Error("Self-dispatch denied: orchestrator cannot call task(orchestrator).")
      }
    },
  }
}

export default NoSelfDispatch
