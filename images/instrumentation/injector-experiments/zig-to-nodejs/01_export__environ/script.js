console.log("Accessing individual environment variables works:");
console.log("PATH:", process.env.PATH);
console.log("VAR3:", process.env.VAR3);
console.log("VAR4:", process.env.VAR4);

console.log("Now accessing process.env, this will segfault");
console.log(process.env);
