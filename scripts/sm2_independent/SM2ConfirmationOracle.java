// TEST ONLY. Scalars and shared keys travel through anonymous pipes, never argv.
// Uses BC's standard confirmation API, not the project's SM3 composition.
import java.io.*;
import java.math.BigInteger;
import java.nio.charset.StandardCharsets;
import java.util.HexFormat;
import org.bouncycastle.asn1.gm.GMNamedCurves;
import org.bouncycastle.asn1.x9.X9ECParameters;
import org.bouncycastle.crypto.agreement.SM2KeyExchange;
import org.bouncycastle.crypto.params.*;
import org.bouncycastle.util.Arrays;

public final class SM2ConfirmationOracle {
    public static void main(String[] args) throws Exception {
        HexFormat hex=HexFormat.of();
        X9ECParameters p=GMNamedCurves.getByName("sm2p256v1");
        ECDomainParameters d=new ECDomainParameters(p.getCurve(),p.getG(),p.getN(),p.getH());
        BufferedReader in=new BufferedReader(new InputStreamReader(System.in,StandardCharsets.US_ASCII));
        String line;
        while((line=in.readLine())!=null) {
            if(line.length()>2048)throw new IllegalArgumentException("request bound");
            String[] f=line.split("\\|",-1);
            if(f.length!=8 || !(f[0].equals("I")||f[0].equals("R")))throw new IllegalArgumentException("request shape");
            boolean initiator=f[0].equals("I");
            ECPrivateKeyParameters s=new ECPrivateKeyParameters(new BigInteger(1,hex.parseHex(f[1])),d);
            ECPrivateKeyParameters e=new ECPrivateKeyParameters(new BigInteger(1,hex.parseHex(f[2])),d);
            ECPublicKeyParameters ps=new ECPublicKeyParameters(p.getCurve().decodePoint(hex.parseHex(f[3])),d);
            ECPublicKeyParameters pe=new ECPublicKeyParameters(p.getCurve().decodePoint(hex.parseHex(f[4])),d);
            SM2KeyExchange k=new SM2KeyExchange();
            k.init(new ParametersWithID(new SM2KeyExchangePrivateParameters(initiator,s,e),hex.parseHex(f[5])));
            byte[] tag=f[7].equals("-")?null:hex.parseHex(f[7]);
            byte[][] result;
            try {
                result=k.calculateKeyWithConfirmation(256,initiator?tag:null,new ParametersWithID(new SM2KeyExchangePublicParameters(ps,pe),hex.parseHex(f[6])));
            } catch(IllegalStateException ex) {
                if(initiator && "confirmation tag mismatch".equals(ex.getMessage())) {System.out.println("REJECTED_S1");continue;}
                throw ex;
            }
            // BC responder API returns expected S2; verification uses BC's
            // constant-time utility explicitly. It is not implicit API checking.
            if(!initiator && tag!=null && !Arrays.constantTimeAreEqual(tag,result[2])) {
                System.out.println("REJECTED_S2");
            } else {
                for(int i=0;i<result.length;i++) {
                    if(i>0)System.out.print("|");System.out.print(hex.formatHex(result[i]));
                }
                System.out.println();
            }
            for(byte[] v:result)java.util.Arrays.fill(v,(byte)0);
        }
    }
}
