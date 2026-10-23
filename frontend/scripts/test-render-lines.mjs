import { readFileSync } from "node:fs";
import { resolve } from "node:path";
import vm from "node:vm";

const code = readFileSync(resolve("src/assets/index.js"), "utf8");
const sandbox = {
  window: {},
  document: {
    getElementById: () => ({ innerHTML: "", addEventListener: () => {}, disabled: false }),
    querySelectorAll: () => [],
    querySelector: () => null
  },
  setTimeout: () => {},
  console
};

vm.createContext(sandbox);
vm.runInContext(code, sandbox);

const source = `spring:
  cloud:
    gateway:
      server:
        webflux:
          routes:
          - id: dxtx-aggregation-server
            uri: lb://dxtx-aggregation-server
          - id: dxtx-pay-server
            uri: lb://dxtx-pay-server
sys:
  auth:
    exclude-paths:
    - /travel/product/recommended/grouping
    - /pay/pay/order/notify
    - /travel/product/detail/**
`;

const item = {
  status: "changed",
  targetExists: true,
  targetDataId: "dxtx-gateway-server-local.yml",
  changes: [
    {
      path: "spring.cloud.gateway.server.webflux.routes",
      kind: "modify",
      listItems: [{ key: "id=dxtx-pay-server", side: "source" }]
    },
    {
      path: "sys.auth.exclude-paths",
      kind: "modify",
      listItems: [
        { key: "/pay/pay/order/notify", side: "source" },
        { key: "/travel/product/detail/**", side: "source" }
      ]
    }
  ]
};

const html = sandbox.renderLines(source, item, "source");
const checks = [
  ["pay route has action", html.includes("id=dxtx-pay-server")],
  ["notify path has action", html.includes("/pay/pay/order/notify")],
  ["detail path has action", html.includes("/travel/product/detail/**")],
  ["source-only hint rendered", (html.match(/源多此项/g) || []).length === 3],
  ["added lines highlighted", (html.match(/line add/g) || []).length === 3],
  ["unchanged list item not highlighted", !html.includes('class="line add "><span class="line-no">14</span><span class="line-text">    - /travel/product/recommended/grouping')]
];

const failed = checks.filter(([, ok]) => !ok);
if (failed.length) {
  console.error(html.split("\n").filter((line) => line.includes("dxtx-pay-server") || line.includes("/pay/pay/order/notify") || line.includes("/travel/product/detail/**") || line.includes("源多此项")).join("\n"));
  console.error(failed.map(([name]) => `failed: ${name}`).join("\n"));
  process.exit(1);
}

console.log("render line tests passed");

const targetContent = `sys:
  auth:
    exclude-paths:
    - /gateway/**
    - /target/only/**
`;
const targetOnlyItem = {
  status: "changed",
  targetExists: true,
  targetDataId: "dxtx-gateway-server-local.yml",
  changes: [{
    path: "sys.auth.exclude-paths",
    kind: "modify",
    listItems: [{ key: "/target/only/**", side: "target" }]
  }]
};
const overlay = sandbox.targetLineOverlay(targetOnlyItem, targetContent);
const overlayChecks = [
  ["target delete action uses x icon", /title="删除目标项"[^>]*>×<\/button>/.test(overlay)],
  ["target keep action uses arrow icon", /title="保留目标项"[^>]*>→<\/button>/.test(overlay)]
];
const overlayFailed = overlayChecks.filter(([, ok]) => !ok);
if (overlayFailed.length) {
  console.error(overlay);
  console.error(overlayFailed.map(([name]) => `failed: ${name}`).join("\n"));
  process.exit(1);
}

console.log("target overlay tests passed");

const publishStateOK = vm.runInContext(`
  state.plan = {
    items: [{
      targetDataId: "demo.yml",
      targetExists: true,
      targetContent: "a: 1\\n",
      previewContent: "a: 2\\n",
      changes: [{ path: "a", kind: "modify" }],
      warnings: [],
      status: "changed"
    }]
  };
  state.drafts = { "demo.yml": "a: 2\\n" };
  state.decisions = { "demo.yml|a": "source" };
  state.selectedTargets = new Set(["demo.yml"]);
  markPublishedItems({ logs: [{ targetDataId: "demo.yml", success: true }] }, { "demo.yml": "a: 2\\n" });
  const published = state.plan.items[0];
  published.status === "unchanged" &&
    published.changes.length === 0 &&
    !hasDraftChange(published) &&
    !state.selectedTargets.has("demo.yml") &&
    Object.keys(state.decisions).length === 0;
`, sandbox);

if (!publishStateOK) {
  console.error("failed: publish success should clear local pending state");
  process.exit(1);
}

console.log("publish state tests passed");
