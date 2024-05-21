// SPDX-FileCopyrightText: Copyright 2024 Dash0 Inc.
// SPDX-License-Identifier: Apache-2.0

const express = require( "express");

const port = parseInt(process.env.PORT || "1207");
const app = express();

app.get("/ohai", (req, res) => {
	console.log("processing request");
	res.json({ message: "We make Observability easy for every developer." });
});

app.listen(port, () => {
	console.log(`listening on port ${port}`);
});
