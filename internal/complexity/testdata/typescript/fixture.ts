/**
 * TypeScript golden fixture of the complexity engine (#2915).
 *
 * Every function exercises one construct family; expected values live in
 * fixture.golden.json and were cross-checked against lizard (CCN) — see
 * golden_test.go for the version and the known differences. There is no
 * reference cognitive tool for TypeScript; the cognitive values follow the
 * Sonar rules as the Go and Python fixtures verify them.
 */

function elseIfChain(a: number): string {
  if (a === 1) {
    return "one";
  } else if (a === 2) {
    return "two";
  } else if (a === 3) {
    return "three";
  } else {
    // comment-only lines do not count toward NLOC
    if (a < 0) {
      return "negative";
    }
  }
  return "many";
}

function switchArms(a: number): number {
  switch (a) {
    case 1:
      if (a > 0) {
        return 1;
      }
      break;
    case 2:
    case 3:
      return 2;
    default:
      if (a < 0) {
        return 3;
      }
  }
  return 0;
}

function nestedLoops(xs: number[]): number {
  let n = 0;
  outer: for (const i of xs) {
    for (let j = 0; j < i; j++) {
      while (n < 100) {
        if (j === 2) {
          continue outer;
        }
        if (j === 3) {
          break;
        }
        n++;
      }
    }
  }
  for (const k in xs) {
    n += Number(k);
  }
  do {
    n--;
  } while (n > 50);
  return n;
}

function tryCatch(path: string): string | null {
  try {
    return path.trim();
  } catch (e) {
    if (e instanceof TypeError) {
      throw e;
    }
    return null;
  } finally {
    console.log("done");
  }
}

function ternaries(a: boolean, b: number, c: number, d: boolean, e: number): number[] {
  const x = a ? b : c;
  const y = a ? b : d ? c : e;
  const z = b ?? c ?? e;
  const w = a?.valueOf() ? 1 : 0;
  return [x, y, z, w];
}

function booleanSequences(a: boolean, b: boolean, c: boolean, d: boolean): number {
  if (a && b && c) {
    return 1;
  }
  if (a && b || c) {
    return 2;
  }
  if (a || b && c) {
    return 3;
  }
  if (a && b || c && d) {
    return 4;
  }
  return 0;
}

function nestedClosures(xs: number[]): () => number {
  return () => {
    let sum = 0;
    const each = function (x: number) {
      if (x > 0) {
        sum += x;
      }
    };
    xs.forEach((x) => each(x));
    return sum;
  };
}

function factorial(n: number): number {
  if (n <= 1) {
    return 1;
  }
  return n * factorial(n - 1);
}

class Server {
  handle(req: string, retries = 3): boolean {
    if (req === "") {
      return false;
    }
    for (let i = 0; i < retries; i++) {
      if (this.send(req)) {
        return true;
      }
    }
    return this.handle("", 0);
  }

  private send(req: string): boolean {
    return req !== "";
  }
}

function* generator(n: number): Generator<number> {
  while (n > 0) {
    yield n--;
  }
}

function plain(): void {}

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
