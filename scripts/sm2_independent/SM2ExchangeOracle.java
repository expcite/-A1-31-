// TEST-ONLY second implementation bridge. No server imports this source.
// Secret inputs and shared results travel only over anonymous process pipes.
import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.math.BigInteger;
import java.nio.charset.StandardCharsets;
import java.util.HexFormat;
import org.bouncycastle.asn1.gm.GMNamedCurves;
import org.bouncycastle.asn1.x9.X9ECParameters;
import org.bouncycastle.crypto.agreement.SM2KeyExchange;
import org.bouncycastle.crypto.params.*;
import org.bouncycastle.jce.provider.BouncyCastleProvider;

public final class SM2ExchangeOracle {
    private static final HexFormat HEX = HexFormat.of();
    public static void main(String[] args) throws Exception {
        if (args.length == 1 && args[0].equals("metadata")) {
            System.out.println("provider=" + new BouncyCastleProvider().getInfo());
            System.out.println("version=" + new BouncyCastleProvider().getVersionStr());
            System.out.println("license=" + org.bouncycastle.LICENSE.licenseText);
            X9ECParameters p = GMNamedCurves.getByName("sm2p256v1");
            System.out.println("curve=sm2p256v1");
            System.out.println("p=" + p.getCurve().getField().getCharacteristic().toString(16));
            System.out.println("a=" + p.getCurve().getA().toBigInteger().toString(16));
            System.out.println("b=" + p.getCurve().getB().toBigInteger().toString(16));
            System.out.println("g=" + HEX.formatHex(p.getG().getEncoded(false)));
            System.out.println("n=" + p.getN().toString(16));
            System.out.println("h=" + p.getH());
            return;
        }
        X9ECParameters p = GMNamedCurves.getByName("sm2p256v1");
        ECDomainParameters domain = new ECDomainParameters(p.getCurve(), p.getG(), p.getN(), p.getH());
        BufferedReader in = new BufferedReader(new InputStreamReader(System.in, StandardCharsets.US_ASCII));
        String line;
        while ((line = in.readLine()) != null) {
            String[] f = line.split("\\|", -1);
            if (f.length != 7 || !(f[0].equals("I") || f[0].equals("R"))) {
                throw new IllegalArgumentException("invalid bounded oracle request");
            }
            ECPrivateKeyParameters s = new ECPrivateKeyParameters(new BigInteger(1, HEX.parseHex(f[1])), domain);
            ECPrivateKeyParameters e = new ECPrivateKeyParameters(new BigInteger(1, HEX.parseHex(f[2])), domain);
            ECPublicKeyParameters ps = new ECPublicKeyParameters(p.getCurve().decodePoint(HEX.parseHex(f[3])), domain);
            ECPublicKeyParameters pe = new ECPublicKeyParameters(p.getCurve().decodePoint(HEX.parseHex(f[4])), domain);
            SM2KeyExchange exchange = new SM2KeyExchange();
            exchange.init(new ParametersWithID(new SM2KeyExchangePrivateParameters(f[0].equals("I"), s, e), HEX.parseHex(f[5])));
            // Bouncy Castle kLen is bits; the production Go adapter uses 32 bytes.
            byte[] key = exchange.calculateKey(256, new ParametersWithID(new SM2KeyExchangePublicParameters(ps, pe), HEX.parseHex(f[6])));
            System.out.println(HEX.formatHex(key));
            java.util.Arrays.fill(key, (byte)0);
        }
    }
}
