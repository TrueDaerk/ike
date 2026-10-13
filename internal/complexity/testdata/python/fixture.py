"""Python golden fixture of the complexity engine (#2915).

Every function exercises one construct family; expected values live in
fixture.golden.json and were cross-checked against lizard (CCN) and complexipy
(cognitive) — see golden_test.go for the versions and the known differences.
"""


def else_if_chain(a):
    if a == 1:
        return "one"
    elif a == 2:
        return "two"
    elif a == 3:
        return "three"
    else:
        # comment-only lines do not count toward NLOC
        if a < 0:
            return "negative"
    return "many"


def match_arms(a):
    match a:
        case 1:
            if a > 0:
                return 1
        case 2 | 3:
            return 2
        case _:
            if a < 0:
                return 3
    return 0


def nested_loops(xs):
    n = 0
    for i in xs:
        for j in range(i):
            while n < 100:
                if j == 2:
                    continue
                if j == 3:
                    break
                n += 1
    return n


def try_except(path):
    try:
        with open(path) as fh:
            return fh.read()
    except FileNotFoundError:
        return ""
    except (OSError, ValueError) as exc:
        if exc.args:
            raise
        return None


def ternaries(a, b, c, d, e):
    x = a if b else c
    y = a if b else (c if d else e)
    return x, y


def boolean_sequences(a, b, c, d):
    if a and b and c:
        return 1
    if a and b or c:
        return 2
    if a or b and c:
        return 3
    if a and b or c and d:
        return 4
    return 0


def nested_closures(xs):
    def total():
        acc = []
        each = lambda x: acc.append(x) if x > 0 else None
        for x in xs:
            each(x)
        return len(acc)
    return total


def factorial(n):
    if n <= 1:
        return 1
    return n * factorial(n - 1)


class Server:
    def handle(self, req, retries=3):
        if not req:
            return False
        for attempt in range(retries):
            if self.send(req):
                return True
        return False

    def send(self, req):
        return bool(req)


def plain():
    pass


# documented carries a comment above its header; it lies outside the unit,
# and the docstring is a string statement, not a comment: density 0 (#2934).
def documented(x):
    """A docstring is not a comment."""
    return x + 1


def comment_heavy(x):
    # first explain
    # then explain more
    y = x * 2
    # and again
    # and once more
    return y
