/**
 * JavaScript golden fixture of the complexity engine (#2915). JavaScript is
 * measured through the TypeScript spec (the TSX grammar serves every JS/TS
 * dialect; `.js` resolves to the `typescript` language id).
 *
 * Expected values live in fixture.golden.json and were cross-checked against
 * lizard (CCN) — see golden_test.go.
 */

function elseIfChain(a) {
  if (a === 1) {
    return "one";
  } else if (a === 2) {
    return "two";
  } else {
    // comment-only lines do not count toward NLOC
    if (a < 0) {
      return "negative";
    }
  }
  return "many";
}

function switchArms(a) {
  switch (a) {
    case 1:
      if (a > 0) {
        return 1;
      }
      break;
    case 2:
      return 2;
    default:
      return 0;
  }
}

function nestedLoops(xs) {
  let n = 0;
  outer: for (const x of xs) {
    for (let j = 0; j < x; j++) {
      if (j === 2) {
        continue outer;
      }
      n++;
    }
  }
  return n;
}

function tryCatch(fn) {
  try {
    return fn();
  } catch (e) {
    return e.message ? e.message : "";
  }
}

function booleanSequences(a, b, c) {
  if (a && b && c) {
    return 1;
  }
  if (a && b || c) {
    return 2;
  }
  return a ?? b ?? c;
}

const nestedClosures = (xs) => {
  const each = (x) => (x > 0 ? x : 0);
  return xs.map((x) => each(x)).filter(function keep(x) {
    return x !== 0;
  });
};

function factorial(n) {
  if (n <= 1) {
    return 1;
  }
  return n * factorial(n - 1);
}

class Server {
  handle(req, retries = 3) {
    if (req === "") {
      return false;
    }
    while (retries-- > 0) {
      if (this.send(req)) {
        return true;
      }
    }
    return false;
  }

  send(req) {
    return req !== "";
  }
}

module.exports = { elseIfChain, switchArms, nestedLoops, tryCatch, booleanSequences, nestedClosures, factorial, Server };

/**
 * documented carries a JSDoc above its header; it lies outside the unit,
 * so the comment density stays 0 (#2934).
 */
function documented(x) {
  return x + 1;
}

function commentHeavy(x) {
  // first explain
  // then explain more
  const y = x * 2;
  /* a block
     comment */
  return y;
  /* closing note
     runs into the last line */ }
