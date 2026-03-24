/*
 * This file is part of the Krypton project, licensed under the Apache License v2.0
 *
 * Copyright (C) 2021-2023 KryptonMC and the contributors of the Krypton project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
package org.kryptonmc.api.util

import net.kyori.adventure.util.RGBLike
import org.jetbrains.annotations.Contract

/**
 * An immutable RGB colour value.
 *
 * The [value] is the packed 24-bit integer: `0xRRGGBB`.
 */
@JvmInline
public value class Color(public val value: Int) : RGBLike {

    /** The red component (0–255). */
    public override fun red(): Int = (value shr 16) and 0xFF

    /** The green component (0–255). */
    public override fun green(): Int = (value shr 8) and 0xFF

    /** The blue component (0–255). */
    public override fun blue(): Int = value and 0xFF

    public companion object {

        /** Pure black. */
        @JvmStatic public fun black(): Color = Color(0x000000)

        /** Pure white. */
        @JvmStatic public fun white(): Color = Color(0xFFFFFF)

        /**
         * Creates a [Color] from [red], [green], and [blue] components (0–255 each).
         */
        @JvmStatic
        @Contract("_, _, _ -> new", pure = true)
        public fun of(red: Int, green: Int, blue: Int): Color =
            Color(((red and 0xFF) shl 16) or ((green and 0xFF) shl 8) or (blue and 0xFF))

        /**
         * Creates a [Color] from a packed 24-bit RGB integer.
         */
        @JvmStatic
        @Contract("_ -> new", pure = true)
        public fun of(value: Int): Color = Color(value)
    }
}
