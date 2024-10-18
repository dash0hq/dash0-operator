const nodeOptions = process.env["NODE_OPTIONS"] || "";

if (!nodeOptions.includes("--require /__dash0__/instrumentation/node.js/node_modules/@dash0hq/opentelemetry")) {
    console.error(`No Dash0 distro require found in NODE_OPTIONS value: '${nodeOptions}'`)
    process.exit(1);
}