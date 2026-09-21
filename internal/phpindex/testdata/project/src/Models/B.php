<?php

namespace App\Models;

use App\Traits\A;
use App\Traits\C;
use App\Traits\X;
use App\Base\ParentModel;

/**
 * B consumes the traits A and C, and X under an alias.
 */
class B extends ParentModel implements \JsonSerializable
{
    use A, C;
    use X {
        foo as bar;
    }

    /** Does the abc thing. */
    public function abc(int $times = 1): string
    {
        return str_repeat('abc', $times);
    }

    public function jsonSerialize(): mixed
    {
        return [];
    }
}
