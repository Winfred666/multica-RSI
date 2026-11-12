import { z } from "zod";
import { parseWithFallback } from "../api/schema";
import type { TimelineEntry } from "../types";

const SOPOutputSchema = z.object({
  status: z.string().nullable().default(null),
  instruction: z.string(),
  args: z.array(z.string()).optional(),
  error: z.object({ message: z.string() }).optional(),
  next_step: z.array(z.object({ instruction: z.string(), command: z.string() })).default([]),
});
const SOPRemoteSchema = z.object({ args: z.array(z.string()), que_comment_id: z.string().optional() });

type SOPOption = { instruction: string; command: string; sop: string; argv: string[] };
export type SOPQuestion = Omit<z.infer<typeof SOPOutputSchema>, "next_step"> & { kind: "render"; sop: string; next_step: SOPOption[] };
type SOPRemote = z.infer<typeof SOPRemoteSchema> & { kind: "remote"; sop: string };

// Decode the shell words emitted by shlex.join, without evaluating shell syntax.
function commandWords(command: string): string[] | null {
  const words: string[] = [];
  let end = 0;
  for (const match of command.matchAll(/(?:[^\s'"\\]+|'[^']*'|"(?:\\.|[^"\\])*"|\\.)+/g)) {
    if (command.slice(end, match.index).trim()) return null;
    words.push(match[0].replace(/'([^']*)'|"((?:\\.|[^"\\])*)"|\\(.)/g,
      (_, single: string | undefined, double: string | undefined, escaped: string) =>
        single ?? double?.replace(/\\([\\"$`])/g, "$1") ?? escaped));
    end = match.index + match[0].length;
  }
  return command.slice(end).trim() ? null : words;
}

export function parseSOPComment(entry: TimelineEntry): SOPQuestion | SOPRemote | null {
  if (entry.type !== "comment" || entry.deleted_at) return null;
  const content = (entry.content ?? "").trim();
  const remote = /^\/note !sop-remote (\S+) ([\s\S]+)$/.exec(content);
  if (remote) {
    try {
      const payload = parseWithFallback<z.infer<typeof SOPRemoteSchema> | null>(JSON.parse(remote[2]!), SOPRemoteSchema, null, { endpoint: "SoP remote comment" });
      return payload ? { ...payload, kind: "remote", sop: remote[1]! } : null;
    } catch {
      return null;
    }
  }
  const render = /^(?:\/note|\[@[^\]]+\]\(mention:\/\/agent\/[^)]+\)) !sop-render (\S+) [^\r\n]+\r?\n([\s\S]+)$/.exec(content);
  if (!render) return null;
  try {
    const output = parseWithFallback<z.infer<typeof SOPOutputSchema> | null>(JSON.parse(render[2]!), SOPOutputSchema, null, {
      endpoint: "SoP output comment",
    });
    if (!output) return null;
    const options: SOPOption[] = [];
    for (const option of output.next_step) {
      const words = commandWords(option.command);
      if (!words || words.slice(0, 3).join(" ") !== "multica sop run" || !words[3]) return null;
      options.push({ ...option, sop: words[3], argv: words.slice(4) });
    }
    return { ...output, kind: "render", sop: render[1]!, next_step: options };
  } catch {
    return null;
  }
}

export function sopParameters(option: SOPOption): string[] {
  return [...new Set(option.argv.flatMap((arg) => [...arg.matchAll(/<([^<>]+)>/g)].map((match) => match[1]!)))];
}

export function sopAnswerContent(option: SOPOption, values: Record<string, string>, questionId: string): string {
  const args = option.argv.map((arg) => arg.replace(/<([^<>]+)>/g, (_, name: string) => values[name] ?? ""));
  return `/note !sop-remote ${option.sop} ${JSON.stringify({ args, que_comment_id: questionId })}`;
}

export function sopSelection(question: SOPQuestion): { index: number; values: Record<string, string> } | undefined {
  const { args } = question;
  if (!args) return undefined;
  for (const [index, option] of question.next_step.entries()) {
    const values: Record<string, string> = {};
    if (option.argv.length !== args.length) continue;
    const matches = option.argv.every((template, i) => {
      const names: string[] = [];
      const pattern = template.split(/(<[^<>]+>)/g).map((part) => {
        if (part.startsWith("<") && part.endsWith(">")) { names.push(part.slice(1, -1)); return "([\\s\\S]*)"; }
        return part.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
      }).join("");
      const result = new RegExp(`^${pattern}$`).exec(args[i]!);
      return !!result && names.every((name, j) => {
        const value = result[j + 1]!;
        if (values[name] !== undefined && values[name] !== value) return false;
        values[name] = value;
        return true;
      });
    });
    if (matches) return { index, values };
  }
  return undefined;
}
