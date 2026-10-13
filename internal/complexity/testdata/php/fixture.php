<?php
/**
 * PHP golden fixture of the complexity engine (#2915).
 *
 * Every function exercises one construct family; expected values live in
 * fixture.golden.json and were cross-checked against lizard (CCN) — see
 * golden_test.go for the version and the known differences. There is no
 * reference cognitive tool for PHP; the cognitive values follow the Sonar
 * rules as the Go and Python fixtures verify them.
 */

function elseIfChain(int $a): string
{
    if ($a == 1) {
        return "one";
    } elseif ($a == 2) {
        return "two";
    } else if ($a == 3) {
        return "three";
    } else {
        // comment-only lines do not count toward NLOC
        if ($a < 0) {
            return "negative";
        }
    }
    return "many";
}

function switchAndMatch(int $a): int
{
    switch ($a) {
        case 1:
            if ($a > 0) {
                return 1;
            }
            break;
        case 2:
        case 3:
            return 2;
        default:
            if ($a < 0) {
                return 3;
            }
    }
    return match ($a) {
        1, 2 => 10,
        3 => 30,
        default => 0,
    };
}

function nestedLoops(array $xs): int
{
    $n = 0;
    foreach ($xs as $i => $x) {
        for ($j = 0; $j < $i; $j++) {
            while ($n < 100) {
                if ($j == 2) {
                    continue 3;
                }
                if ($j == 3) {
                    break;
                }
                $n++;
            }
        }
    }
    do {
        $n--;
    } while ($n > 50);
    return $n;
}

function tryCatch(string $path): ?string
{
    try {
        return file_get_contents($path);
    } catch (RuntimeException $e) {
        return null;
    } catch (LogicException | TypeError $e) {
        if ($e->getCode()) {
            throw $e;
        }
        return "";
    } finally {
        clearstatcache();
    }
}

function ternaries($a, $b, $c, $d, $e)
{
    $x = $a ? $b : $c;
    $y = $a ? $b : ($c ? $d : $e);
    $z = $a ?: $b;
    $w = $a ?? $b ?? $c;
    return [$x, $y, $z, $w];
}

function booleanSequences(bool $a, bool $b, bool $c, bool $d): int
{
    if ($a && $b && $c) {
        return 1;
    }
    if ($a && $b || $c) {
        return 2;
    }
    if ($a and $b or $c) {
        return 3;
    }
    if ($a || $b && $c && $d) {
        return 4;
    }
    return 0;
}

function nestedClosures(array $xs): callable
{
    return function () use ($xs) {
        $sum = 0;
        $each = fn($x) => $x > 0 ? $x : 0;
        foreach ($xs as $x) {
            $sum += $each($x);
        }
        return $sum;
    };
}

function factorial(int $n): int
{
    if ($n <= 1) {
        return 1;
    }
    return $n * factorial($n - 1);
}

function labelledJumps(array $xs): int
{
    $i = 0;
    foreach ($xs as $x) {
        if ($x < 0) {
            break;
        }
        $i++;
    }
    if ($i > 10) {
        goto done;
    }
    done:
    return $i;
}

class Server
{
    public function handle(string $req, int $retries = 3): bool
    {
        if ($req === "") {
            return false;
        }
        for ($i = 0; $i < $retries; $i++) {
            if ($this->send($req)) {
                return true;
            }
        }
        return $this->handle("", 0);
    }

    private function send(string $req): bool
    {
        return $req !== "";
    }
}

trait Loggable
{
    public function log(string $msg): void
    {
        echo $msg;
    }
}

function plain(): void
{
}

/**
 * documented carries a PHPDoc above its header; it lies outside the unit,
 * so the comment density stays 0 (#2934).
 */
function documented(int $x): int
{
    return $x + 1;
}

function commentHeavy(int $x): int
{
    // first explain
    // then explain more
    $y = $x * 2;
    /* a block
       comment */
    return $y;
    /* closing note
       runs into the last line */ }
