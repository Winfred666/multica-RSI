// @vitest-environment node
import { expect, it } from "vitest";
import type { TimelineEntry } from "../types";
import { parseSOPComment, sopAnswerContent, sopParameters, sopSelection } from "./sop-question";

const output = { status: "review", instruction: "Choose the next step.", next_step: [
  { instruction: "Approve", command: "multica sop run review approve --summary '<summary>'" },
  { instruction: "Rework", command: "multica sop run review rework --reason '<reason>'" },
] };
const entry = (content: string, fields: Partial<TimelineEntry> = {}): TimelineEntry => ({
  id: "comment", type: "comment", actor_type: "agent", actor_id: "agent", comment_type: "comment",
  created_at: "2026-11-13T18:00:00Z", content, ...fields,
});
const render = (state: unknown, prefix = "/note") => `${prefix} !sop-render review {"issue_id":"target","thread":"thread","id":"agent"}\n${JSON.stringify(state)}`;
const question = (state = output) => {
  const parsed = parseSOPComment(entry(render(state)));
  if (parsed?.kind !== "render") throw new Error("Expected a render card");
  return parsed;
};

it("parses pending and supplied args without consulting other comments or changing authorship", () => {
  expect(question().args).toBeUndefined();
  expect(parseSOPComment(entry(render(output), { actor_type: "member" }))).toMatchObject({ kind: "render", status: "review" });
  expect(parseSOPComment(entry(render(output, "[@Renderer](mention://agent/agent)")))).toMatchObject({ sop: "review" });
  const answered = parseSOPComment(entry(render({ ...output, args: ["approve", "--summary", "Looks good"] })))!;
  if (answered.kind !== "render") throw new Error("Expected an answered card");
  expect(sopSelection(answered)).toEqual({ index: 0, values: { summary: "Looks good" } });
  expect(sopSelection({ ...answered, args: ["unknown"] })).toBeUndefined();
  expect(sopSelection({ ...answered, args: [], next_step: [{ instruction: "Run", command: "multica sop run review", sop: "review", argv: [] }] })).toEqual({ index: 0, values: {} });
  for (const content of [JSON.stringify(output), "plain output", render(output).replace(/\n.*/, "\n{broken"),
    render({ ...output, args: [1] }), render({ ...output, next_step: [{ instruction: "Run", command: "echo hello" }] })]) {
    expect(parseSOPComment(entry(content))).toBeNull();
  }
  expect(parseSOPComment(entry(render({ status: "FINISHED", instruction: "Done." })))).toMatchObject({ next_step: [] });
});

it("fills placeholders and serializes JSON arguments linked to the question", () => {
  const option = question().next_step[0]!;
  expect(sopParameters(option)).toEqual(["summary"]);
  const summary = "It's done; $(echo no)\nnext line";
  const content = sopAnswerContent(option, { summary }, "question-id");
  expect(content).toBe(`/note !sop-remote review ${JSON.stringify({ args: ["approve", "--summary", summary], que_comment_id: "question-id" })}`);
  expect(parseSOPComment(entry(content))).toMatchObject({ args: ["approve", "--summary", summary], que_comment_id: "question-id" });
  expect(sopAnswerContent(option, { summary: "" }, "question-id")).toBe('/note !sop-remote review {"args":["approve","--summary",""],"que_comment_id":"question-id"}');
  const quoted = question({ ...output, next_step: [{ instruction: "Run", command: `multica sop run review note 'it'"'"'s done' ''` }] });
  expect(quoted.next_step[0]?.argv).toEqual(["note", "it's done", ""]);
});

it("recognizes remote invocations without inventing execution status, leaving malformed and ordinary comments alone", () => {
  for (const args of [[], ["approve", "--summary", "Looks good"]]) {
    const content = `/note !sop-remote review ${JSON.stringify({ args })}`;
    expect(parseSOPComment(entry(content))).toEqual({ kind: "remote", sop: "review", args });
    expect(parseSOPComment(entry(content, { deleted_at: "now" }))).toBeNull();
  }
  for (const content of ["/note !sop review []", "/note !sop-remote", "/note !sop-remote review", "/note !sop-remote review approve --summary 'Looks good'",
    '/note !sop-remote review {"args":[1]}', '/note !sop-remote review {"args":[],"que_comment_id":1}', '/note !sop-remote review {}', '/note !sop-remote review {broken']) {
    expect(parseSOPComment(entry(content))).toBeNull();
  }
});
