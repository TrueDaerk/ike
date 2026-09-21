<?php

namespace App\Services;

use App\Models\B as Model;
use App\Traits\{X as Mixin, Y};

class Report extends Model
{
    use Mixin;

    public function render(): string
    {
        return '';
    }
}
