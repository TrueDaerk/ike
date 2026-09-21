<?php

namespace App\Models;

use App\Traits\X;
use App\Traits\Y;

final class D
{
    use X, Y {
        X::foo insteadof Y;
        Y::foo as protected yFoo;
    }

    public const VERSION = '1';
}
