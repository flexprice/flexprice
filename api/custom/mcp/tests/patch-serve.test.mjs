import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, test } from "node:test";

const patchScript = process.env.PATCH_SCRIPT;

const generated = `import { buildAnnotationFilter } from "../../tools.js";
import { buildSDK } from "../../tools.js";

async function startStreamableHTTP(cliFlags) {
  const app = express();

  app.use((req, res, next) => {
    res.header("Access-Control-Allow-Headers", "*");
    next();
  });

  app.use(express.json());

  app.post("/mcp", async (req, res) => {
    const { server: mcpServer } = createMCPServer({
      getSDK: () =>
        buildSDK(headers, cliFlags, cliFlags["disable-static-auth"], logger),
    });

    await mcpServer.connect(transport as Transport);
  });
}
`;

const expectedLines = [
  'import { buildSDK } from "../../tools.js";',
  'res.header("Access-Control-Allow-Headers", "*");',
  "app.use(express.json());",
  'buildSDK(headers, cliFlags, cliFlags["disable-static-auth"], logger),',
  "await mcpServer.connect(transport as Transport);",
];

function runPatch(file) {
  const { status, stderr } = spawnSync(process.execPath, [patchScript, file], { encoding: "utf8" });
  return { status, stderr };
}

function patching(content) {
  const dir = mkdtempSync(join(tmpdir(), "patch-serve-"));
  const file = join(dir, "impl.ts");
  writeFileSync(file, content);
  return {
    run: () => ({ ...runPatch(file), content: readFileSync(file, "utf8") }),
    cleanup: () => rmSync(dir, { recursive: true }),
  };
}

describe("patch-mcp-serve", { skip: !patchScript && "PATCH_SCRIPT is not set" }, () => {
  test("a second run leaves an already patched file unchanged", () => {
    const { run, cleanup } = patching(generated);
    const first = run();
    const second = run();
    cleanup();
    assert.equal(first.status, 0);
    assert.notEqual(first.content, generated);
    assert.equal(second.status, 0);
    assert.equal(second.content, first.content);
  });

  test("the environment tool is added before the server starts answering", () => {
    const { run, cleanup } = patching(generated);
    const { content } = run();
    cleanup();
    const added = content.indexOf("registerEnvironmentTool(mcpServer, headers, cliFlags, logger);");
    assert.notEqual(added, -1, content);
    assert.ok(added < content.indexOf("await mcpServer.connect("), content);
  });

  const changes = {
    without: (line) => generated.replace(line, "// changed by the generator"),
    "with a second": (line) => `${generated}\n${line}\n`,
  };
  for (const [change, apply] of Object.entries(changes)) {
    for (const line of expectedLines) {
      test(`a generated file ${change} \`${line}\` fails, names the line and is left alone`, () => {
        const changed = apply(line);
        const { run, cleanup } = patching(changed);
        const result = run();
        cleanup();
        assert.equal(result.status, 1);
        assert.ok(result.stderr.includes(`patch-mcp-serve: ${line}`), result.stderr);
        assert.equal(result.content, changed);
      });
    }
  }

  test("a serve file that is no longer where it was fails and names the path", () => {
    const missing = join(tmpdir(), "patch-serve-missing", "impl.ts");
    const result = runPatch(missing);
    assert.equal(result.status, 1);
    assert.ok(result.stderr.includes(`patch-mcp-serve: ${missing}`), result.stderr);
  });
});
