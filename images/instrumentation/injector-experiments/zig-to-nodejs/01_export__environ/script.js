console.log("accessing individual environment variables:");
console.log("PATH:", process.env.PATH);
console.log("VAR3:", process.env.VAR3);
console.log("VAR4:", process.env.VAR4);

console.log("accessing process.env");
console.log(process.env);

console.log("writing to process.env");
process.env.NEW_VAR1 = "new value 1";
process.env.NEW_VAR2 = "new value 2";

console.log("accessing individual environment variables:");
console.log("PATH:", process.env.PATH);
console.log("VAR3:", process.env.VAR3);
console.log("VAR4:", process.env.VAR4);
console.log("NEW_VAR1:", process.env.NEW_VAR1);
console.log("NEW_VAR2:", process.env.NEW_VAR2);

console.log("accessing process.env");
console.log(process.env);
