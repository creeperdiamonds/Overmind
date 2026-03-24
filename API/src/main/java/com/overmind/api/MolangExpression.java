package com.overmind.api;

/**
 * Evaluates a subset of the Molang (Mojang Animation Language) expression language.
 *
 * <h3>Supported constructs</h3>
 * <ul>
 *   <li>Numeric literals (integer and decimal)</li>
 *   <li>Arithmetic: {@code +}, {@code -}, {@code *}, {@code /} (division by zero → 0)</li>
 *   <li>Unary negation and explicit positive sign</li>
 *   <li>Parentheses</li>
 *   <li>Variables: {@code query.anim_time} / {@code q.anim_time}, {@code query.life_time},
 *       {@code this} (evaluates to 0 — bone-relative value, not supported without full context)</li>
 *   <li>Math functions: {@code math.sin}, {@code math.cos}, {@code math.abs},
 *       {@code math.floor}, {@code math.ceil}, {@code math.round}, {@code math.sqrt},
 *       {@code math.clamp}, {@code math.lerp}</li>
 *   <li>Ternary conditional: {@code condition ? trueExpr : falseExpr}</li>
 * </ul>
 *
 * <p>Angles for {@code math.sin} / {@code math.cos} are in <em>degrees</em>, matching
 * the Bedrock Molang specification.
 *
 * <p>Unsupported constructs (complex Molang scripts, return statements, etc.) evaluate
 * to 0 rather than throwing, to satisfy the "fallback gracefully" acceptance criterion.
 */
public class MolangExpression {

    /** A pre-compiled expression that always returns 0. */
    public static final MolangExpression ZERO = new MolangExpression("0");

    private final String source;

    private MolangExpression(String source) {
        this.source = source.trim();
    }

    /**
     * Parses a Molang expression string.
     * Always succeeds; malformed input will produce 0 at evaluation time.
     */
    public static MolangExpression parse(String expression) {
        if (expression == null || expression.trim().isEmpty()) return ZERO;
        return new MolangExpression(expression);
    }

    /**
     * Evaluates the expression with the given animation context.
     *
     * @param animTime seconds elapsed since the animation started
     * @return the computed floating-point result
     */
    public double evaluate(double animTime) {
        try {
            return new Evaluator(source, animTime).parseTernary();
        } catch (Exception e) {
            return 0; // malformed expression: return neutral value
        }
    }

    @Override
    public String toString() {
        return "MolangExpression{" + source + "}";
    }

    // ── Inner evaluator (stateful, not thread-safe by design — create per call) ──

    private static final class Evaluator {
        private final String s;
        private final double animTime;
        private int pos;

        Evaluator(String s, double animTime) {
            this.s = s;
            this.animTime = animTime;
            this.pos = 0;
        }

        double parseTernary() {
            double val = parseAddSub();
            skip();
            if (pos < s.length() && s.charAt(pos) == '?') {
                pos++;
                double trueVal = parseAddSub();
                skip();
                if (pos < s.length() && s.charAt(pos) == ':') pos++;
                double falseVal = parseAddSub();
                return val != 0 ? trueVal : falseVal;
            }
            return val;
        }

        private double parseAddSub() {
            double r = parseMulDiv();
            while (true) {
                skip();
                if (pos >= s.length()) break;
                char op = s.charAt(pos);
                if      (op == '+') { pos++; r += parseMulDiv(); }
                else if (op == '-') { pos++; r -= parseMulDiv(); }
                else break;
            }
            return r;
        }

        private double parseMulDiv() {
            double r = parseUnary();
            while (true) {
                skip();
                if (pos >= s.length()) break;
                char op = s.charAt(pos);
                if (op == '*') { pos++; r *= parseUnary(); }
                else if (op == '/') {
                    pos++;
                    double d = parseUnary();
                    r = (d == 0) ? 0 : r / d;
                }
                else break;
            }
            return r;
        }

        private double parseUnary() {
            skip();
            if (pos < s.length() && s.charAt(pos) == '-') { pos++; return -parsePrimary(); }
            if (pos < s.length() && s.charAt(pos) == '+') { pos++; }
            return parsePrimary();
        }

        private double parsePrimary() {
            skip();
            if (pos >= s.length()) return 0;
            char c = s.charAt(pos);

            // Parenthesised sub-expression
            if (c == '(') {
                pos++;
                double v = parseTernary();
                skip();
                if (pos < s.length() && s.charAt(pos) == ')') pos++;
                return v;
            }

            // Numeric literal
            if (c == '.' || Character.isDigit(c)) return parseNumber();

            // Identifier: variable or function call
            if (Character.isLetter(c) || c == '_') {
                String name = parseIdent();
                skip();
                if (pos < s.length() && s.charAt(pos) == '(') {
                    pos++; // consume '('
                    // Parse first argument
                    double arg1 = parseTernary();
                    double arg2 = 0;
                    skip();
                    if (pos < s.length() && s.charAt(pos) == ',') {
                        pos++;
                        arg2 = parseTernary();
                    }
                    // Parse possible third argument (math.clamp)
                    double arg3 = 0;
                    skip();
                    if (pos < s.length() && s.charAt(pos) == ',') {
                        pos++;
                        arg3 = parseTernary();
                    }
                    skip();
                    if (pos < s.length() && s.charAt(pos) == ')') pos++;
                    return applyFn(name, arg1, arg2, arg3);
                }
                return resolveVar(name);
            }

            return 0;
        }

        private double parseNumber() {
            int start = pos;
            while (pos < s.length() && (Character.isDigit(s.charAt(pos)) || s.charAt(pos) == '.')) pos++;
            String tok = s.substring(start, pos);
            try { return Double.parseDouble(tok); } catch (NumberFormatException e) { return 0; }
        }

        private String parseIdent() {
            int start = pos;
            while (pos < s.length()) {
                char c = s.charAt(pos);
                if (Character.isLetterOrDigit(c) || c == '_' || c == '.') pos++;
                else break;
            }
            return s.substring(start, pos).toLowerCase();
        }

        private void skip() {
            while (pos < s.length() && Character.isWhitespace(s.charAt(pos))) pos++;
        }

        private double applyFn(String fn, double a, double b, double c) {
            switch (fn) {
                case "math.sin":   return Math.sin(Math.toRadians(a));   // Bedrock: degrees
                case "math.cos":   return Math.cos(Math.toRadians(a));
                case "math.abs":   return Math.abs(a);
                case "math.floor": return Math.floor(a);
                case "math.ceil":  return Math.ceil(a);
                case "math.round": return Math.round(a);
                case "math.sqrt":  return Math.sqrt(Math.max(0, a));
                case "math.pi":    return Math.PI;
                case "math.clamp": return Math.max(b, Math.min(c, a));   // clamp(val, min, max)
                case "math.lerp":  return a + (b - a) * c;               // lerp(a, b, t)
                case "math.pow":   return Math.pow(a, b);
                default:           return a;
            }
        }

        private double resolveVar(String name) {
            switch (name) {
                case "query.anim_time":
                case "q.anim_time":    return animTime;
                case "query.life_time":
                case "q.life_time":    return animTime;
                case "math.pi":        return Math.PI;
                case "this":           return 0;
                default:               return 0;
            }
        }
    }
}
